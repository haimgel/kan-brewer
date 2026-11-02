package sync

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The fields of Kanister's cr.kanister.io/v1alpha1 ActionSet that kan-brewer uses. Kanister's Go module is not
// imported on purpose; see CLAUDE.md.

var actionSetResource = schema.GroupVersionResource{Group: "cr.kanister.io", Version: "v1alpha1", Resource: "actionsets"}

const actionSetStateComplete = "complete"

type actionSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              actionSetSpec    `json:"spec"`
	Status            *actionSetStatus `json:"status,omitempty"`
}

type actionSetSpec struct {
	Actions []actionSpec `json:"actions"`
}

type actionSpec struct {
	Name      string          `json:"name"`
	Blueprint string          `json:"blueprint,omitempty"`
	Object    objectReference `json:"object"`
}

type objectReference struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type actionSetStatus struct {
	State string `json:"state"`
}
