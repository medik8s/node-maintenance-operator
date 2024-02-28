package utils

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

const (
	// events reasons
	EventReasonBeginMaintenance   = "BeginMaintenance"
	EventReasonEvictingPods       = "EvictingPods"
	EventReasonFailedMaintenance  = "FailedMaintenance"
	EventReasonSucceedMaintenance = "SucceedMaintenance"
	EventReasonUncordonNode       = "UncordonNode"
	EventReasonRemovedMaintenance = "RemovedMaintenance"

	// events messages
	EventMessageBeginMaintenance   = "Begin maintenance"
	EventMessageEvictingPods       = "Evicting pods"
	EventMessageFailedMaintenance  = "Failed maintenance"
	EventMessageSucceedMaintenance = "Node maintenance was succeed"
	EventMessageUncordonNode       = "Uncordon node"
	EventMessageRemovedMaintenance = "Removed maintenance"
)

// NormalEvent will record an event with type Normal and fixed message.
func NormalEvent(recorder record.EventRecorder, object runtime.Object, reason, message string) {
	recorder.Event(object, corev1.EventTypeNormal, reason, message)
}

// WarningEvent will record an event with type Warning and fixed message.
func WarningEvent(recorder record.EventRecorder, object runtime.Object, reason, message string) {
	recorder.Event(object, corev1.EventTypeWarning, reason, message)
}
