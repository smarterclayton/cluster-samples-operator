package cvo

import (
	"fmt"
	"time"

	"github.com/blang/semver"

	"github.com/golang/glog"
	"github.com/google/uuid"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-version-operator/lib/resourcemerge"
	cvv1 "github.com/openshift/cluster-version-operator/pkg/apis/config.openshift.io/v1"
	osv1 "github.com/openshift/cluster-version-operator/pkg/apis/operatorstatus.openshift.io/v1"
	"github.com/openshift/cluster-version-operator/pkg/cincinnati"
)

// syncAvailableUpdates attempts to retrieve the latest updates and update the status of the ClusterVersion
// object. It will set the RetrievedUpdates condition. Updates are only checked if it has been more than
// the minimumUpdateCheckInterval since the last check.
func (optr *Operator) syncAvailableUpdates(config *cvv1.ClusterVersion) error {
	upstream := optr.defaultUpstreamServer
	if config.Spec.Upstream != nil {
		upstream = string(*config.Spec.Upstream)
	}
	channel := config.Spec.Channel

	// updates are only checked at most once per minimumUpdateCheckInterval or if the generation changes
	u := optr.getAvailableUpdates()
	if u != nil && u.Upstream == upstream && u.Channel == channel && u.RecentlyChanged(optr.minimumUpdateCheckInterval) {
		glog.V(4).Infof("Available updates were recently retrieved, will try later.")
		return nil
	}

	updates, condition := calculateAvailableUpdatesStatus(string(config.Spec.ClusterID), upstream, channel, optr.releaseVersion)
	optr.setAvailableUpdates(&availableUpdates{
		Upstream:  upstream,
		Channel:   config.Spec.Channel,
		Updates:   updates,
		Condition: condition,
	})
	// requeue
	optr.queue.Add(optr.queueKey())
	return nil
}

type availableUpdates struct {
	Upstream string
	Channel  string

	At time.Time

	Updates   []cvv1.Update
	Condition osv1.ClusterOperatorStatusCondition
}

func (u *availableUpdates) RecentlyChanged(interval time.Duration) bool {
	return u.At.After(time.Now().Add(-interval))
}

func (u *availableUpdates) NeedsUpdate(original *cvv1.ClusterVersion) *cvv1.ClusterVersion {
	if u == nil {
		return nil
	}
	if equality.Semantic.DeepEqual(u.Updates, original.Status.AvailableUpdates) &&
		equality.Semantic.DeepEqual(u.Condition, resourcemerge.FindOperatorStatusCondition(original.Status.Conditions, u.Condition.Type)) {
		return nil
	}

	config := original.DeepCopy()
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, u.Condition)
	config.Status.AvailableUpdates = u.Updates
	return config
}

// setAvailableUpdates updates the currently calculated version of updates.
func (optr *Operator) setAvailableUpdates(u *availableUpdates) {
	if u != nil {
		u.At = time.Now()
	}

	optr.statusLock.Lock()
	defer optr.statusLock.Unlock()
	optr.availableUpdates = u
}

// getAvailableUpdates returns the current calculated version of updates. It
// may be nil.
func (optr *Operator) getAvailableUpdates() *availableUpdates {
	optr.statusLock.Lock()
	defer optr.statusLock.Unlock()
	return optr.availableUpdates
}

func calculateAvailableUpdatesStatus(clusterID, upstream, channel, version string) ([]cvv1.Update, osv1.ClusterOperatorStatusCondition) {
	if len(upstream) == 0 {
		return nil, osv1.ClusterOperatorStatusCondition{
			Type: cvv1.RetrievedUpdates, Status: osv1.ConditionFalse, Reason: "NoUpstream",
			Message: "No upstream server has been set to retrieve updates.",
		}
	}

	if len(version) == 0 {
		return nil, osv1.ClusterOperatorStatusCondition{
			Type: cvv1.RetrievedUpdates, Status: osv1.ConditionFalse, Reason: "NoCurrentVersion",
			Message: "The cluster version does not have a semantic version assigned and cannot calculate valid upgrades.",
		}
	}

	currentVersion, err := semver.Parse(version)
	if err != nil {
		glog.V(2).Infof("Unable to parse current semantic version %q: %v", version, err)
		return nil, osv1.ClusterOperatorStatusCondition{
			Type: cvv1.RetrievedUpdates, Status: osv1.ConditionFalse, Reason: "InvalidCurrentVersion",
			Message: "The current cluster version is not a valid semantic version and cannot be used to calculate upgrades.",
		}
	}

	updates, err := checkForUpdate(clusterID, upstream, channel, currentVersion)
	if err != nil {
		glog.V(2).Infof("Upstream server %s could not return available updates: %v", upstream, err)
		return nil, osv1.ClusterOperatorStatusCondition{
			Type: cvv1.RetrievedUpdates, Status: osv1.ConditionFalse, Reason: "RemoteFailed",
			Message: fmt.Sprintf("Unable to retrieve available updates: %v", err),
		}
	}

	var cvoUpdates []cvv1.Update
	for _, update := range updates {
		cvoUpdates = append(cvoUpdates, cvv1.Update{
			Version: update.Version.String(),
			Payload: update.Payload,
		})
	}

	return cvoUpdates, osv1.ClusterOperatorStatusCondition{
		Type:   cvv1.RetrievedUpdates,
		Status: osv1.ConditionTrue,

		LastTransitionTime: metav1.Now(),
	}
}

func checkForUpdate(clusterID, upstream, channel string, currentVersion semver.Version) ([]cincinnati.Update, error) {
	uuid, err := uuid.Parse(string(clusterID))
	if err != nil {
		return nil, err
	}
	if len(upstream) == 0 {
		return nil, fmt.Errorf("no upstream URL set for cluster version")
	}
	return cincinnati.NewClient(uuid).GetUpdates(upstream, channel, currentVersion)
}
