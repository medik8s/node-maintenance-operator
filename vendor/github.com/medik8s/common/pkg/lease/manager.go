package lease

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	log "github.com/sirupsen/logrus"

	coordv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Manager interface {
	//RequestLease will create a lease with leaseDuration if it does not exist or extend existing lease duration to leaseDuration.
	//It'll return an error in case it can't do either (for example if the lease is already taken).
	RequestLease(ctx context.Context, obj client.Object, leaseDuration time.Duration) error
	//InvalidateLease will release the lease.
	InvalidateLease(ctx context.Context, obj client.Object) error
	//GetLease will try to fetch a lease.
	//It'll return an error in case it can't (for example if the lease does not exist or is already taken).
	GetLease(ctx context.Context, obj client.Object) (*coordv1.Lease, error)
}

type manager struct {
	client.Client
	holderIdentity string
	namespace      string
	log            logr.Logger
}

func (l *manager) RequestLease(ctx context.Context, obj client.Object, leaseDuration time.Duration) error {
	return l.requestLease(ctx, obj, leaseDuration)
}

func (l *manager) GetLease(ctx context.Context, obj client.Object) (*coordv1.Lease, error) {
	return l.getLease(ctx, obj)
}

func (l *manager) InvalidateLease(ctx context.Context, obj client.Object) error {
	return l.invalidateLease(ctx, obj)
}

func NewManager(cl client.Client, holderIdentity string, namespace string) Manager {
	return NewManagerWithCustomLogger(cl, holderIdentity, namespace, ctrl.Log.WithName("leaseManager"))

}

func NewManagerWithCustomLogger(cl client.Client, holderIdentity string, namespace string, log logr.Logger) Manager {
	return &manager{
		Client:         cl,
		holderIdentity: holderIdentity,
		namespace:      namespace,
		log:            log,
	}
}

func (l *manager) createLease(ctx context.Context, obj client.Object, duration time.Duration) error {
	log.Info("create lease")
	owner := makeExpectedOwnerOfLease(obj)
	microTimeNow := metav1.NowMicro()

	lease := &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:            generateLeaseName(obj),
			Namespace:       l.namespace,
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: coordv1.LeaseSpec{
			HolderIdentity:       &l.holderIdentity,
			LeaseDurationSeconds: pointer.Int32(int32(duration.Seconds())),
			AcquireTime:          &microTimeNow,
			RenewTime:            &microTimeNow,
			LeaseTransitions:     pointer.Int32(0),
		},
	}

	if err := l.Client.Create(ctx, lease); err != nil {
		l.log.Error(err, "failed to create lease")
		return err
	}
	return nil
}

func (l *manager) requestLease(ctx context.Context, obj client.Object, leaseDuration time.Duration) error {
	log.Info("request lease")
	lease, err := l.getLease(ctx, obj)
	//couldn't get the lease try to create one
	if err != nil {
		if err = l.createLease(ctx, obj, leaseDuration); err != nil {
			l.log.Error(err, "couldn't create lease")
			return err
		} else {
			return nil
		}
	}

	needUpdateLease := false
	setAcquireAndLeaseTransitions := false
	currentTime := metav1.NowMicro()
	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == l.holderIdentity {
		needUpdateLease, setAcquireAndLeaseTransitions = needUpdateOwnedLease(lease, currentTime, leaseDuration)
		if needUpdateLease {
			log.Infof("renew lease owned by %s setAcquireTime=%t", l.holderIdentity, setAcquireAndLeaseTransitions)

		}
	} else {
		// can't take over the lease if it is currently valid.
		if isValidLease(lease, currentTime.Time) {
			return fmt.Errorf("can't update valid lease held by different owner")
		}
		needUpdateLease = true

		log.Info("taking over foreign lease")
		setAcquireAndLeaseTransitions = true
	}

	if needUpdateLease {
		if setAcquireAndLeaseTransitions {
			lease.Spec.AcquireTime = &currentTime
			if lease.Spec.LeaseTransitions != nil {
				*lease.Spec.LeaseTransitions += int32(1)
			} else {
				lease.Spec.LeaseTransitions = pointer.Int32(1)
			}
		}
		owner := makeExpectedOwnerOfLease(obj)
		lease.ObjectMeta.OwnerReferences = []metav1.OwnerReference{*owner}
		lease.Spec.HolderIdentity = &l.holderIdentity
		lease.Spec.LeaseDurationSeconds = pointer.Int32(int32(leaseDuration.Seconds()))
		lease.Spec.RenewTime = &currentTime
		if err := l.Client.Update(ctx, lease); err != nil {
			log.Errorf("Failed to update the lease. obj %s error: %v", obj.GetName(), err)
			return err
		}
	}

	return nil
}

func (l *manager) invalidateLease(ctx context.Context, obj client.Object) error {
	log.Info("invalidating lease")
	lease, err := l.getLease(ctx, obj)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := l.Client.Delete(ctx, lease); err != nil {
		log.Error(err, "failed to delete lease to be invalidated")
		return err
	}

	return nil
}

func makeExpectedOwnerOfLease(obj client.Object) *metav1.OwnerReference {

	return &metav1.OwnerReference{
		APIVersion: obj.GetObjectKind().GroupVersionKind().Version,
		Kind:       obj.GetObjectKind().GroupVersionKind().Kind,
		Name:       obj.GetName(),
		UID:        obj.GetUID(),
	}
}

func leaseDueTime(lease *coordv1.Lease) time.Time {
	return lease.Spec.RenewTime.Time.Add(time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second)
}

func needUpdateOwnedLease(lease *coordv1.Lease, currentTime metav1.MicroTime, requestedLeaseDuration time.Duration) (bool, bool) {

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

	deadline := currentTime.Add(requestedLeaseDuration)

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

func (l *manager) getLease(ctx context.Context, obj client.Object) (*coordv1.Lease, error) {
	log.Info("getting lease")
	nName := apitypes.NamespacedName{Namespace: l.namespace, Name: generateLeaseName(obj)}
	lease := &coordv1.Lease{}

	if err := l.Client.Get(ctx, nName, lease); err != nil {
		if !errors.IsNotFound(err) {
			l.log.Error(err, "couldn't fetch lease")
		}
		return nil, err
	}

	return lease, nil
}

func generateLeaseName(obj client.Object) string {
	return fmt.Sprintf("%s-%s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
}
