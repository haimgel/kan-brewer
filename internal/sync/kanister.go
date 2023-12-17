package sync

import (
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
