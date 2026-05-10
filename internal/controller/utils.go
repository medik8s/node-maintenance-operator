package controller

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/medik8s/node-maintenance-operator/api/v1beta1"
)

// ContainsString checks if the string array contains the given string.
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// RemoveString removes the given string from the string array if exists.
func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return result
}

// GetPodNameList returns a list of pod names from a pod list
func GetPodNameList(pods []corev1.Pod) (result []string) {
	for _, pod := range pods {
		result = append(result, pod.ObjectMeta.Name)
	}
	return result
}

// GetPodRefList returns a list of pod references.
func GetPodRefList(pods []corev1.Pod) (result []v1beta1.PodReference) {
	for _, pod := range pods {
		result = append(result, v1beta1.PodReference{
			Namespace: pod.Namespace,
			Name:      pod.Name,
		})
	}
	return result

}
