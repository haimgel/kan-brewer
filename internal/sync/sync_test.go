package sync

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/haimgel/kan-brewer/internal/config"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const kanisterNamespace = "kanister"

var baseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func seededActionSet(t *testing.T, name, generateName string, age int, state string, managed bool) *unstructured.Unstructured {
	t.Helper()
	as := &actionSet{
		TypeMeta: metav1.TypeMeta{APIVersion: actionSetResource.GroupVersion().String(), Kind: "ActionSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			GenerateName:      generateName,
			Namespace:         kanisterNamespace,
			CreationTimestamp: metav1.NewTime(baseTime.Add(time.Duration(age) * time.Hour)),
		},
	}
	if managed {
		as.Labels = map[string]string{config.ManagedByLabel: config.AppId}
	}
	if state != "" {
		as.Status = &actionSetStatus{State: state}
	}
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(as)
	if err != nil {
		t.Fatal(err)
	}
	return &unstructured.Unstructured{Object: content}
}

func TestProcess(t *testing.T) {
	const group = "auto-bp-a-app-"
	kube := k8sfake.NewClientset(
		&apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "app", Annotations: map[string]string{config.BlueprintAnnotationName: "bp-a,bp-b"}}},
		&apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "plain"}},
		&apiv1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
			Name: "data", Namespace: "app", Annotations: map[string]string{config.BlueprintAnnotationName: "bp-pvc"}}},
	)
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{actionSetResource: "ActionSetList"},
		seededActionSet(t, "pending", group, 0, "", true),
		seededActionSet(t, "old-complete", group, 1, "complete", true),
		seededActionSet(t, "failed", group, 2, "failed", true),
		seededActionSet(t, "mid-complete", group, 3, "complete", true),
		seededActionSet(t, "recent-1", group, 4, "complete", true),
		seededActionSet(t, "recent-2", group, 5, "complete", true),
		seededActionSet(t, "unmanaged", group, 0, "complete", false),
	)

	// The fake tracker implements neither generateName nor creationTimestamp; mimic the API server.
	var created []*unstructured.Unstructured
	dyn.PrependReactor("create", "actionsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		obj := action.(k8stesting.CreateAction).GetObject().(*unstructured.Unstructured)
		obj.SetName(fmt.Sprintf("%s%d", obj.GetGenerateName(), len(created)))
		obj.SetCreationTimestamp(metav1.NewTime(baseTime.Add(100 * time.Hour)))
		created = append(created, obj.DeepCopy())
		return false, nil, nil
	})

	s := &Synchronizer{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Client:        kube,
		DynamicClient: dyn,
		Cfg:           config.Config{ActionSetNamespace: kanisterNamespace, KeepCompletedActionSets: 3},
	}
	if err := s.Process(); err != nil {
		t.Fatal(err)
	}

	wantSpecs := map[string]map[string]any{
		"auto-bp-a-app-": {"actions": []any{map[string]any{
			"name": "backup", "blueprint": "bp-a", "object": map[string]any{"kind": "Namespace", "name": "app"}}}},
		"auto-bp-b-app-": {"actions": []any{map[string]any{
			"name": "backup", "blueprint": "bp-b", "object": map[string]any{"kind": "Namespace", "name": "app"}}}},
		"auto-bp-pvc-app-data-": {"actions": []any{map[string]any{
			"name": "backup", "blueprint": "bp-pvc", "object": map[string]any{"kind": "Pvc", "name": "data", "namespace": "app"}}}},
	}
	if len(created) != len(wantSpecs) {
		t.Fatalf("created %d ActionSets, want %d", len(created), len(wantSpecs))
	}
	for _, obj := range created {
		want, ok := wantSpecs[obj.GetGenerateName()]
		if !ok {
			t.Errorf("unexpected ActionSet generateName %q", obj.GetGenerateName())
			continue
		}
		if obj.GetAPIVersion() != "cr.kanister.io/v1alpha1" || obj.GetKind() != "ActionSet" {
			t.Errorf("%s: got %s %s", obj.GetName(), obj.GetAPIVersion(), obj.GetKind())
		}
		if obj.GetNamespace() != kanisterNamespace || obj.GetLabels()[config.ManagedByLabel] != config.AppId {
			t.Errorf("%s: namespace %q, labels %v", obj.GetName(), obj.GetNamespace(), obj.GetLabels())
		}
		if !equality.Semantic.DeepEqual(obj.Object["spec"], want) {
			t.Errorf("%s: spec %v, want %v", obj.GetName(), obj.Object["spec"], want)
		}
	}

	remaining, err := dyn.Resource(actionSetResource).Namespace(kanisterNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, item := range remaining.Items {
		if item.GetGenerateName() == group {
			names = append(names, item.GetName())
		}
	}
	slices.Sort(names)
	// Of the oldest four in the group (seven minus three kept), only the complete ones go.
	want := []string{"auto-bp-a-app-0", "failed", "pending", "recent-1", "recent-2", "unmanaged"}
	if !slices.Equal(names, want) {
		t.Errorf("remaining ActionSets %v, want %v", names, want)
	}
}
