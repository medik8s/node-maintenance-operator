package lease

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"

	log "github.com/sirupsen/logrus"

	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	leaseApiPackage = "coordination.k8s.io/v1"
)

type Manager interface {
	CreateOrGetLease(ctx context.Context, node *corev1.Node, duration time.Duration, holderIdentity string, namespace string) (*coordv1.Lease, bool, error)
	UpdateLease(ctx context.Context, node *corev1.Node, lease *coordv1.Lease, currentTime *metav1.MicroTime, leaseDuration, leaseDeadline time.Duration, holderIdentity string) (bool, error)
	InvalidateLease(ctx context.Context, nodeName string, leaseNamespace string) error
}

type manager struct {
	client.Client
	log logr.Logger
}

func (l *manager) CreateOrGetLease(ctx context.Context, node *corev1.Node, duration time.Duration, holderIdentity string, namespace string) (*coordv1.Lease, bool, error) {
	return l.createOrGetExistingLease(ctx, node, duration, holderIdentity, namespace)
}

func (l *manager) UpdateLease(ctx context.Context, node *corev1.Node, lease *coordv1.Lease, currentTime *metav1.MicroTime, leaseDuration, leaseDeadline time.Duration, holderIdentity string) (bool, error) {
	return l.updateLease(ctx, node, lease, currentTime, leaseDuration, leaseDeadline, holderIdentity)
}

func (l *manager) InvalidateLease(ctx context.Context, nodeName string, leaseNamespace string) error {
	return l.invalidateLease(ctx, nodeName, leaseNamespace)
}

func NewManager(cl client.Client) Manager {
	return NewManagerWithCustomLogger(cl, ctrl.Log.WithName("leaseManager"))

}

func NewManagerWithCustomLogger(cl client.Client, log logr.Logger) Manager {
	return &manager{
		Client: cl,
		log:    log,
	}
}

func (l *manager) createOrGetExistingLease(_ context.Context, node *corev1.Node, duration time.Duration, holderIdentity string, leaseNamespace string) (*coordv1.Lease, bool, error) {
	owner := makeExpectedOwnerOfLease(node)
	microTimeNow := metav1.NowMicro()

	lease := &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:            node.ObjectMeta.Name,
			Namespace:       leaseNamespace,
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: coordv1.LeaseSpec{
			HolderIdentity:       &holderIdentity,
			LeaseDurationSeconds: pointer.Int32(int32(duration.Seconds())),
			AcquireTime:          &microTimeNow,
			RenewTime:            &microTimeNow,
			LeaseTransitions:     pointer.Int32(0),
		},
	}

	if err := l.Client.Create(context.TODO(), lease); err != nil {
		if errors.IsAlreadyExists(err) {

			nodeName := node.ObjectMeta.Name
			key := apitypes.NamespacedName{Namespace: leaseNamespace, Name: nodeName}

			if err := l.Client.Get(context.TODO(), key, lease); err != nil {
				return nil, false, err
			}
			return lease, true, nil
		}
		return nil, false, err
	}
	return lease, false, nil
}

func (l *manager) updateLease(_ context.Context, node *corev1.Node, lease *coordv1.Lease, currentTime *metav1.MicroTime, leaseDuration, leaseDeadline time.Duration, holderIdentity string) (bool, error) {
	needUpdateLease := false
	setAcquireAndLeaseTransitions := false
	updateAlreadyOwnedLease := false

	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == holderIdentity {
		needUpdateLease, setAcquireAndLeaseTransitions = needUpdateOwnedLease(lease, *currentTime, leaseDeadline)
		if needUpdateLease {
			updateAlreadyOwnedLease = true

			log.Infof("renew lease owned by nmo setAcquireTime=%t", setAcquireAndLeaseTransitions)

		}
	} else {
		// can't update the lease if it is currently valid.
		if isValidLease(lease, currentTime.Time) {
			return false, fmt.Errorf("can't update valid lease held by different owner")
		}
		needUpdateLease = true

		log.Info("taking over foreign lease")
		setAcquireAndLeaseTransitions = true
	}

	if needUpdateLease {
		if setAcquireAndLeaseTransitions {
			lease.Spec.AcquireTime = currentTime
			if lease.Spec.LeaseTransitions != nil {
				*lease.Spec.LeaseTransitions += int32(1)
			} else {
				lease.Spec.LeaseTransitions = pointer.Int32(1)
			}
		}
		owner := makeExpectedOwnerOfLease(node)
		lease.ObjectMeta.OwnerReferences = []metav1.OwnerReference{*owner}
		lease.Spec.HolderIdentity = &holderIdentity
		lease.Spec.LeaseDurationSeconds = pointer.Int32(int32(leaseDuration.Seconds()))
		lease.Spec.RenewTime = currentTime
		if err := l.Client.Update(context.TODO(), lease); err != nil {
			log.Errorf("Failed to update the lease. node %s error: %v", node.Name, err)
			return updateAlreadyOwnedLease, err
		}
	}

	return false, nil
}

func (l *manager) invalidateLease(_ context.Context, nodeName string, leaseNamespace string) error {
	log.Info("Lease object supported, invalidating lease")
	nName := apitypes.NamespacedName{Namespace: leaseNamespace, Name: nodeName}
	lease := &coordv1.Lease{}

	if err := l.Client.Get(context.TODO(), nName, lease); err != nil {

		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	lease.Spec.AcquireTime = nil
	lease.Spec.LeaseDurationSeconds = nil
	lease.Spec.RenewTime = nil
	lease.Spec.LeaseTransitions = nil

	if err := l.Client.Update(context.TODO(), lease); err != nil {
		return err
	}
	return nil
}

func makeExpectedOwnerOfLease(node *corev1.Node) *metav1.OwnerReference {
	return &metav1.OwnerReference{
		APIVersion: corev1.SchemeGroupVersion.WithKind("Node").Version,
		Kind:       corev1.SchemeGroupVersion.WithKind("Node").Kind,
		Name:       node.ObjectMeta.Name,
		UID:        node.ObjectMeta.UID,
	}
}

func leaseDueTime(lease *coordv1.Lease) time.Time {
	return lease.Spec.RenewTime.Time.Add(time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second)
}

func needUpdateOwnedLease(lease *coordv1.Lease, currentTime metav1.MicroTime, leaseDeadline time.Duration) (bool, bool) {

	if lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil {
		log.Info("empty renew time or duration in sec")
		return true, true
	}
	dueTime := leaseDueTime(lease)

	// if lease expired right now, then both update the lease and the acquire time (second rvalue)
	// if the acquire time has been previously nil
	if dueTime.Before(currentTime.Time) {
		return true, lease.Spec.AcquireTime == nil
	}

	deadline := currentTime.Add(leaseDeadline)

	// about to expire, update the lease but not the acquired time (second value)
	return dueTime.Before(deadline), false
}

func isValidLease(lease *coordv1.Lease, currentTime time.Time) bool {

	if lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil {
		return false
	}

	renewTime := (*lease.Spec.RenewTime).Time
	dueTime := leaseDueTime(lease)

	// valid lease if: due time not in the past and renew time not in the future
	return !dueTime.Before(currentTime) && !renewTime.After(currentTime)
}
