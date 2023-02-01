package controllers

import (
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sdiscovery "k8s.io/client-go/discovery"
	k8sfakeclient "k8s.io/client-go/kubernetes/fake"
)

func ExpectEqualWithNil(actual, expected interface{}, description string) {
	// heads up: interfaces representing a pointer are not nil when the pointer is nil
	if expected == nil || (reflect.ValueOf(expected).Kind() == reflect.Ptr && reflect.ValueOf(expected).IsNil()) {
		// BeNil() handles pointers correctly
		ExpectWithOffset(1, actual).To(BeNil(), description)
	} else {
		// compare unix time, precision of MicroTime is sometimes different
		if e, ok := expected.(*metav1.MicroTime); ok {
			expected = e.Unix()
			if actual != nil && reflect.ValueOf(actual).Kind() == reflect.Ptr && !reflect.ValueOf(actual).IsNil() {
				actual = actual.(*metav1.MicroTime).Unix()
			}
		}
		ExpectWithOffset(1, actual).To(Equal(expected), description)
	}
}

type TestCaseDefinition struct {
	info        string
	mock        *FakeClient
	expectedErr error
	expectedRes bool
}

var _ = Describe("Leases", func() {
	Context("API support", func() {
		testCases := []TestCaseDefinition{
			{
				"should fail with error if Discovery API returns error",
				&FakeClient{clientType: FakeClientReturnError},
				fmt.Errorf("ServerGroupsFails"),
				false,
			},
			{
				"should succeed if lease API is supported",
				&FakeClient{clientType: FakeClientReturnLeasePackage},
				nil,
				true,
			},
			{
				"should fail if lease API is not supported",
				&FakeClient{clientType: FakeClientReturnWrongPackage},
				nil,
				false,
			},
		}
		for _, c := range testCases {
			It(c.info, func() {
				By(c.info)

				isLeaseSupported, err := checkLeaseSupportedInternal(c.mock)

				ExpectEqualWithNil(err, c.expectedErr, "error should match")
				Expect(isLeaseSupported).To(Equal(c.expectedRes))

			})
		}
	})
})

type FakeDiscoveryThatReturnsError struct {
	k8sdiscovery.DiscoveryInterface
}

func (self *FakeDiscoveryThatReturnsError) ServerGroups() (*metav1.APIGroupList, error) {
	return nil, fmt.Errorf("ServerGroupsFails")
}

type FakeDiscovery struct {
	k8sdiscovery.DiscoveryInterface
	groupList *metav1.APIGroupList
}

func (self *FakeDiscovery) ServerGroups() (*metav1.APIGroupList, error) {
	return self.groupList, nil
}

const (
	FakeClientReturnError        = 0
	FakeClientReturnLeasePackage = 1
	FakeClientReturnWrongPackage = 2
)

type FakeClient struct {
	k8sfakeclient.Clientset
	clientType int
}

func (self *FakeClient) Discovery() k8sdiscovery.DiscoveryInterface {
	if self.clientType == FakeClientReturnError {
		return &FakeDiscoveryThatReturnsError{}
	}
	if self.clientType == FakeClientReturnWrongPackage {
		return &FakeDiscovery{groupList: &metav1.APIGroupList{
			Groups: []metav1.APIGroup{{
				Name: "lease",
				Versions: []metav1.GroupVersionForDiscovery{{
					GroupVersion: LeaseApiPackage + "notQuite",
					Version:      "v1",
				}},
			}},
		}}
	}
	if self.clientType == FakeClientReturnLeasePackage {
		return &FakeDiscovery{groupList: &metav1.APIGroupList{
			Groups: []metav1.APIGroup{{
				Name: "lease",
				Versions: []metav1.GroupVersionForDiscovery{{
					GroupVersion: LeaseApiPackage,
					Version:      "v1",
				}},
			}},
		}}
	}
	return nil
}
