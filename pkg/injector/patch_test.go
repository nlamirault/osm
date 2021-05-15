package injector

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	mapset "github.com/deckarep/golang-set"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	tassert "github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/openservicemesh/osm/pkg/certificate/providers/tresor"
	"github.com/openservicemesh/osm/pkg/configurator"
	k8s "github.com/openservicemesh/osm/pkg/kubernetes"
	"github.com/openservicemesh/osm/pkg/tests"
)

var _ = Describe("Test all patch operations", func() {

	// Setup all constants and variables needed for the tests
	proxyUUID := uuid.New()
	const (
		namespace = "-namespace-"
		podName   = "-pod-name-"
	)

	Context("test createPatch() function", func() {
		It("creates a patch", func() {
			client := fake.NewSimpleClientset()
			mockCtrl := gomock.NewController(GinkgoT())
			mockConfigurator := configurator.NewMockConfigurator(mockCtrl)
			mockNsController := k8s.NewMockController(mockCtrl)
			mockNsController.EXPECT().GetNamespace(namespace).Return(&corev1.Namespace{})
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
			}
			_, err := client.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			wh := &mutatingWebhook{
				kubeClient:          client,
				kubeController:      mockNsController,
				certManager:         tresor.NewFakeCertManager(mockConfigurator),
				configurator:        mockConfigurator,
				nonInjectNamespaces: mapset.NewSet(),
			}

			pod := tests.NewPodFixture(namespace, podName, tests.BookstoreServiceAccountName, nil)
			pod.Annotations = nil
			mockConfigurator.EXPECT().GetEnvoyLogLevel().Return("").Times(1)
			mockConfigurator.EXPECT().GetEnvoyImage().Return("").Times(1)
			mockConfigurator.EXPECT().GetInitContainerImage().Return("").Times(1)
			mockConfigurator.EXPECT().IsPrivilegedInitContainer().Return(false).Times(1)
			mockConfigurator.EXPECT().GetOutboundIPRangeExclusionList().Return(nil).Times(1)
			mockConfigurator.EXPECT().GetOutboundPortExclusionList().Return(nil).Times(1)
			mockConfigurator.EXPECT().GetProxyResources().Return(corev1.ResourceRequirements{}).Times(1)

			req := &admissionv1.AdmissionRequest{Namespace: namespace}
			jsonPatches, err := wh.createPatch(&pod, req, proxyUUID)

			Expect(err).ToNot(HaveOccurred())

			expectedJSONPatches := `[` +
				// Add Volumes
				`{"op":addOperation,` +
				`"path":"/spec/volumes",` +
				`"value":[{"name":"envoy-bootstrap-config-volume","secret":{"secretName":"envoy-bootstrap-config-proxy-uuid"}}]},` +

				// Add Init Container
				`{"op":addOperation,` +
				`"path":"/spec/initContainers",` +
				`"value":[{"name":"osm-init","env":[{"name":"OSM_PROXY_UID","value":"1500"},` +
				`{"name":"OSM_ENVOY_INBOUND_PORT","value":"15003"},{"name":"OSM_ENVOY_OUTBOUND_PORT","value":"15001"}],` +
				`"resources":{},"securityContext":{"capabilities":{addOperation:["NET_ADMIN"]}}}]},` +

				// Add Envoy Container
				`{"op":addOperation,"path":"/spec/containers",` +
				`"value":[{"name":"envoy","command":["envoy"],` +
				`"args":["--log-level","","--config-path","/etc/envoy/bootstrap.yaml","--service-node","bookstore","--service-cluster","bookstore.-namespace-","--bootstrap-version 3"],` +
				`"ports":[{"name":"proxy-admin","containerPort":15000},{"name":"proxy-inbound","containerPort":15003},{"name":"proxy-metrics","containerPort":15010}],` +
				`"resources":{},"volumeMounts":[{"name":"envoy-bootstrap-config-volume","readOnly":true,"mountPath":"/etc/envoy"}],` +
				`"imagePullPolicy":"Always",` +
				`"securityContext":{"runAsUser":1500}}]},{"op":addOperation,"path":"/metadata/annotations",` +
				`"value":{"prometheus.io/scrape":"true"}},` +

				// Add Prometheus Port Annotation
				`{"op":addOperation,"path":"/metadata/annotations/prometheus.io~1port","value":"15010"},` +

				// Add Prometheus Path Annotation
				`{"op":addOperation,"path":"/metadata/annotations/prometheus.io~1path","value":"/stats/prometheus"},` +

				// Add Envoy UID Label
				`{"op":replaceOperation,"path":"/metadata/labels/osm-proxy-uuid","value":"proxy-uuid"}` +

				`]`

			Expect(string(jsonPatches)).ToNot(Equal(expectedJSONPatches),
				fmt.Sprintf("Actual: %s", jsonPatches))
		})
	})
})

func TestMergePortExclusionLists(t *testing.T) {
	assert := tassert.New(t)

	testCases := []struct {
		name                              string
		podOutboundPortExclusionList      []int
		globalOutboundPortExclusionList   []int
		expectedOutboundPortExclusionList []int
	}{
		{
			name:                              "overlap in global and pod outbound exclusion list",
			podOutboundPortExclusionList:      []int{6060, 7070},
			globalOutboundPortExclusionList:   []int{6060, 8080},
			expectedOutboundPortExclusionList: []int{6060, 7070, 8080},
		},
		{
			name:                              "no overlap in global and pod outbound exclusion list",
			podOutboundPortExclusionList:      []int{6060, 7070},
			globalOutboundPortExclusionList:   []int{8080},
			expectedOutboundPortExclusionList: []int{6060, 7070, 8080},
		},
		{
			name:                              "pod outbound exclusion list is nil",
			podOutboundPortExclusionList:      nil,
			globalOutboundPortExclusionList:   []int{8080},
			expectedOutboundPortExclusionList: []int{8080},
		},
		{
			name:                              "global outbound exclusion list is nil",
			podOutboundPortExclusionList:      []int{6060, 7070},
			globalOutboundPortExclusionList:   nil,
			expectedOutboundPortExclusionList: []int{6060, 7070},
		},
		{
			name:                              "no global or pod level outbound exclusion list",
			podOutboundPortExclusionList:      nil,
			globalOutboundPortExclusionList:   nil,
			expectedOutboundPortExclusionList: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := mergePortExclusionLists(tc.podOutboundPortExclusionList, tc.globalOutboundPortExclusionList)
			assert.ElementsMatch(tc.expectedOutboundPortExclusionList, actual)
		})
	}
}
