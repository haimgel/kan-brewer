package main

import (
	"context"
	"fmt"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"log/slog"
	"os"
	"sort"
	"strings"
)

type ActionSet struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   metav1.ObjectMeta `json:"metadata"`
	Spec       ActionSetSpec     `json:"spec"`
}

type ActionSetSpec struct {
	Actions []Action `json:"actions"`
}

type Action struct {
	Name      string                `json:"name"`
	Blueprint string                `json:"blueprint"`
	Object    apiv1.ObjectReference `json:"object"`
}

const BlueprintAnnotationName = "backup.haim.dev/kanister-blueprints"
const ManagedByLabel = "app.kubernetes.io/managed-by"
const AppName = "backup.haim.dev"

type Application struct {
	Logger                  *slog.Logger
	Client                  *kubernetes.Clientset
	DynamicClient           *dynamic.DynamicClient
	ActionSetNamespace      string
	KeepCompletedActionSets int
}

func createClientConfig() (*rest.Config, error) {
	// Create either in-cluster or out-of-cluster config
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)
	return config.ClientConfig()
}

func (app *Application) getNamespaces(ctx context.Context) ([]apiv1.Namespace, error) {
	namespaces, err := app.Client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return namespaces.Items, nil
}

func (app *Application) getPvcs(ctx context.Context, namespaces []apiv1.Namespace) ([]apiv1.PersistentVolumeClaim, error) {
	// Get all PVCs in all namespaces

	var pvcs []apiv1.PersistentVolumeClaim
	for _, namespace := range namespaces {
		namespacePvcs, err := app.Client.CoreV1().PersistentVolumeClaims(namespace.Name).List(
			ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		pvcs = append(pvcs, namespacePvcs.Items...)
	}
	return pvcs, nil
}

func (app *Application) createActionSet(ctx context.Context,
	name string, namespace string, blueprint string, object apiv1.ObjectReference) error {

	gvr := schema.GroupVersionResource{Group: "cr.kanister.io", Version: "v1alpha1", Resource: "actionsets"}
	actionSet := &ActionSet{
		APIVersion: "cr.kanister.io/v1alpha1",
		Kind:       "ActionSet",
		Metadata: metav1.ObjectMeta{
			GenerateName: name + "-",
			Namespace:    namespace,
			Labels: map[string]string{
				ManagedByLabel: AppName,
			},
		},
		Spec: ActionSetSpec{
			Actions: []Action{
				{
					Name:      "backup",
					Blueprint: blueprint,
					Object:    object,
				},
			},
		},
	}
	actionSetMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&actionSet)
	if err != nil {
		return err
	}
	customResource := &unstructured.Unstructured{Object: actionSetMap}
	app.Logger.Info("Creating ActionSet", "name", name, "namespace", namespace, "blueprint", blueprint, "object", object)
	resource, err := app.DynamicClient.Resource(gvr).Namespace(actionSet.Metadata.Namespace).Create(ctx, customResource, metav1.CreateOptions{})
	app.Logger.Info("ActionSet created", "name", resource.GetName(), "namespace", resource.GetNamespace())
	return err
}

func (app *Application) scheduleBackupsForNamespace(ctx context.Context, namespace apiv1.Namespace) error {
	if namespace.Annotations[BlueprintAnnotationName] == "" {
		return nil
	}
	blueprints := strings.Split(namespace.Annotations[BlueprintAnnotationName], ",")
	app.Logger.Info("Processing Namespace", "name", namespace.Name, "blueprints", blueprints)

	for _, blueprint := range blueprints {
		name := fmt.Sprintf("auto-%s-%s", blueprint, namespace.Name)
		err := app.createActionSet(ctx, name, app.ActionSetNamespace, blueprint, apiv1.ObjectReference{
			Kind: "Namespace",
			Name: namespace.Name,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (app *Application) scheduleBackupsForPvc(ctx context.Context, pvc apiv1.PersistentVolumeClaim) error {
	if pvc.Annotations[BlueprintAnnotationName] == "" {
		return nil
	}
	blueprints := strings.Split(pvc.Annotations[BlueprintAnnotationName], ",")
	app.Logger.Info("Processing PVC", "name", pvc.Name, "namespace", pvc.Namespace, "blueprints", blueprints)

	for _, blueprint := range blueprints {
		name := fmt.Sprintf("auto-%s-%s-%s", blueprint, pvc.Namespace, pvc.Name)
		err := app.createActionSet(ctx, name, app.ActionSetNamespace, blueprint, apiv1.ObjectReference{
			Kind:      "Pvc",
			Name:      pvc.Name,
			Namespace: pvc.Namespace,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (app *Application) cleanupActionSets(ctx context.Context) error {
	gvr := schema.GroupVersionResource{Group: "cr.kanister.io", Version: "v1alpha1", Resource: "actionsets"}
	namespace := app.DynamicClient.Resource(gvr).Namespace(app.ActionSetNamespace)
	actionSets, err := namespace.List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", ManagedByLabel, AppName),
	})
	if err != nil {
		return err
	}
	// Group by GenerateName
	groups := make(map[string][]unstructured.Unstructured)
	for _, item := range actionSets.Items {
		generateName := item.GetGenerateName()
		groups[generateName] = append(groups[generateName], item)
	}
	// Process each group separately
	for _, group := range groups {
		// Order by creationTimestamp
		sort.Slice(group, func(i, j int) bool {
			itime := group[i].GetCreationTimestamp().Time
			jtime := group[j].GetCreationTimestamp().Time
			return itime.Before(jtime)
		})

		for i, actionSet := range group {
			state, _, _ := unstructured.NestedString(actionSet.Object, "status", "state")
			if i < len(group)-app.KeepCompletedActionSets && state == "complete" {
				err := namespace.Delete(ctx, actionSet.GetName(), metav1.DeleteOptions{})
				if err == nil {
					app.Logger.Info("ActionSet deleted", "name", actionSet.GetName(),
						"namespace", actionSet.GetNamespace())
				} else {
					app.Logger.Error("Error deleting ActionSet", "name", actionSet.GetName(),
						"namespace", actionSet.GetNamespace(), "error", err)
				}
			}
		}
	}
	return nil
}

func (app *Application) createBackupActionSets() error {
	ctx := context.TODO()

	namespaces, err := app.getNamespaces(ctx)
	if err != nil {
		return err
	}
	app.Logger.Info("Discovered namespaces", "count", len(namespaces))

	for _, namespace := range namespaces {
		err := app.scheduleBackupsForNamespace(ctx, namespace)
		if err != nil {
			return err
		}
	}

	pvcs, err := app.getPvcs(ctx, namespaces)
	if err != nil {
		return err
	}
	app.Logger.Info("Discovered PVCs", "count", len(pvcs))

	for _, pvc := range pvcs {
		err := app.scheduleBackupsForPvc(ctx, pvc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (app *Application) Process() (err error) {
	err = app.createBackupActionSets()
	if err != nil {
		return err
	}
	err = app.cleanupActionSets(context.TODO())
	if err != nil {
		return err
	}
	return nil
}

func NewApplication() (*Application, error) {
	clientConfig, err := createClientConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}
	return &Application{
		Logger:                  slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Client:                  client,
		DynamicClient:           dynamicClient,
		ActionSetNamespace:      "kanister",
		KeepCompletedActionSets: 3,
	}, nil
}

func main() {
	app, err := NewApplication()
	if err != nil {
		app.Logger.Error("Fatal initialization", "error", err)
		os.Exit(1)
	}
	err = app.Process()
	if err != nil {
		app.Logger.Error("Fatal processing error", "error", err)
		os.Exit(1)
	}
}
