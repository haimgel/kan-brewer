package sync

import (
	"context"
	"fmt"
	"github.com/haimgel/kan-brewer/internal/config"
	"github.com/kanisterio/kanister/pkg/apis/cr/v1alpha1"
	kanisterclient "github.com/kanisterio/kanister/pkg/client/clientset/versioned"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"log/slog"
	"sort"
	"strings"
)

type Synchronizer struct {
	Logger         *slog.Logger
	Client         *kubernetes.Clientset
	KanisterClient *kanisterclient.Clientset
	Cfg            config.Config
}

func createClientConfig() (*rest.Config, error) {
	// Create either in-cluster or out-of-cluster config
	kConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)
	return kConfig.ClientConfig()
}

func (s *Synchronizer) getNamespaces(ctx context.Context) ([]apiv1.Namespace, error) {
	namespaces, err := s.Client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return namespaces.Items, nil
}

func (s *Synchronizer) getPvcs(ctx context.Context, namespaces []apiv1.Namespace) ([]apiv1.PersistentVolumeClaim, error) {
	// Get all PVCs in all namespaces

	var pvcs []apiv1.PersistentVolumeClaim
	for _, namespace := range namespaces {
		namespacePvcs, err := s.Client.CoreV1().PersistentVolumeClaims(namespace.Name).List(
			ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		pvcs = append(pvcs, namespacePvcs.Items...)
	}
	return pvcs, nil
}

func (s *Synchronizer) createActionSet(ctx context.Context,
	name string, namespace string, blueprint string, object v1alpha1.ObjectReference) error {

	actionSet := &v1alpha1.ActionSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cr.kanister.io/v1alpha1",
			Kind:       "ActionSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: name + "-",
			Namespace:    namespace,
			Labels: map[string]string{
				config.ManagedByLabel: config.AppId,
			},
		},
		Spec: &v1alpha1.ActionSetSpec{
			Actions: []v1alpha1.ActionSpec{
				{
					Name:      "backup",
					Blueprint: blueprint,
					Object:    object,
				},
			},
		},
	}
	s.Logger.Info("Creating ActionSet", "name", name, "namespace", namespace, "blueprint", blueprint, "object", object)
	resource, err := s.KanisterClient.CrV1alpha1().ActionSets(namespace).Create(ctx, actionSet, metav1.CreateOptions{})
	s.Logger.Info("ActionSet created", "name", resource.GetName(), "namespace", resource.GetNamespace())
	return err
}

func (s *Synchronizer) scheduleBackupsForNamespace(ctx context.Context, namespace apiv1.Namespace) error {
	if namespace.Annotations[config.BlueprintAnnotationName] == "" {
		return nil
	}
	blueprints := strings.Split(namespace.Annotations[config.BlueprintAnnotationName], ",")
	s.Logger.Info("Processing Namespace", "name", namespace.Name, "blueprints", blueprints)

	for _, blueprint := range blueprints {
		name := fmt.Sprintf("auto-%s-%s", blueprint, namespace.Name)
		err := s.createActionSet(ctx, name, s.Cfg.ActionSetNamespace, blueprint, v1alpha1.ObjectReference{
			Kind: "Namespace",
			Name: namespace.Name,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Synchronizer) scheduleBackupsForPvc(ctx context.Context, pvc apiv1.PersistentVolumeClaim) error {
	if pvc.Annotations[config.BlueprintAnnotationName] == "" {
		return nil
	}
	blueprints := strings.Split(pvc.Annotations[config.BlueprintAnnotationName], ",")
	s.Logger.Info("Processing PVC", "name", pvc.Name, "namespace", pvc.Namespace, "blueprints", blueprints)

	for _, blueprint := range blueprints {
		name := fmt.Sprintf("auto-%s-%s-%s", blueprint, pvc.Namespace, pvc.Name)
		err := s.createActionSet(ctx, name, s.Cfg.ActionSetNamespace, blueprint, v1alpha1.ObjectReference{
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

func (s *Synchronizer) cleanupActionSets(ctx context.Context) error {
	namespace := s.KanisterClient.CrV1alpha1().ActionSets(s.Cfg.ActionSetNamespace)
	actionSets, err := namespace.List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", config.ManagedByLabel, config.AppId),
	})
	if err != nil {
		return err
	}
	// Group by GenerateName
	groups := make(map[string][]*v1alpha1.ActionSet)
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
			if int64(i) < int64(len(group))-s.Cfg.KeepCompletedActionSets && actionSet.Status.State == v1alpha1.StateComplete {
				err := namespace.Delete(ctx, actionSet.GetName(), metav1.DeleteOptions{})
				if err == nil {
					s.Logger.Info("ActionSet deleted", "name", actionSet.GetName(),
						"namespace", actionSet.GetNamespace())
				} else {
					s.Logger.Error("Error deleting ActionSet", "name", actionSet.GetName(),
						"namespace", actionSet.GetNamespace(), "error", err)
				}
			}
		}
	}
	return nil
}

func (s *Synchronizer) createBackupActionSets() error {
	ctx := context.TODO()

	namespaces, err := s.getNamespaces(ctx)
	if err != nil {
		return err
	}
	s.Logger.Info("Discovered namespaces", "count", len(namespaces))

	for _, namespace := range namespaces {
		err := s.scheduleBackupsForNamespace(ctx, namespace)
		if err != nil {
			return err
		}
	}

	pvcs, err := s.getPvcs(ctx, namespaces)
	if err != nil {
		return err
	}
	s.Logger.Info("Discovered PVCs", "count", len(pvcs))

	for _, pvc := range pvcs {
		err := s.scheduleBackupsForPvc(ctx, pvc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Synchronizer) Process() (err error) {
	err = s.createBackupActionSets()
	if err != nil {
		return err
	}
	err = s.cleanupActionSets(context.TODO())
	if err != nil {
		return err
	}
	return nil
}

func NewSynchronizer(cfg config.Config, logger *slog.Logger) (*Synchronizer, error) {
	clientConfig, err := createClientConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}
	kanisterClient, err := kanisterclient.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}
	return &Synchronizer{
		Logger:         logger,
		Client:         client,
		KanisterClient: kanisterClient,
		Cfg:            cfg,
	}, nil
}
