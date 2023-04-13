/*
 Copyright 2023 The Kapacity Authors.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
// +kubebuilder:docs-gen:collapse=Apache License

/*
Ideally, we should have one `<kind>_controller_test.go` for each controller scaffolded and called in the `suite_test.go`.
So, let's write our example test for the CronJob controller (`cronjob_controller_test.go.`)
*/

/*
As usual, we start with the necessary imports. We also define some utility variables.
*/
package autoscaling

import (
	"context"
	"time"

	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

const (
	testNamespace string = "default"
	testName      string = "test"
)

// +kubebuilder:docs-gen:collapse=Imports

/*
The first step to writing a simple integration test is to actually create an instance of CronJob you can run tests against.
Note that to create a CronJob, you’ll need to create a stub CronJob struct that contains your CronJob’s specifications.
Note that when we create a stub CronJob, the CronJob also needs stubs of its required downstream objects.
Without the stubbed Job template spec and the Pod template spec below, the Kubernetes API will not be able to
create the CronJob.
*/
var _ = Describe("CronJob controller", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When updating CronJob Status", func() {
		It("Should increase CronJob Status.Active count when new Jobs are created", func() {
			By("By creating a new CronJob")
			ctx := context.Background()
			ihpa := CreateIHPAInstance()
			Expect(k8sClient.Create(ctx, ihpa)).Should(Succeed())

			ihpaKey := types.NamespacedName{Name: testName, Namespace: testNamespace}
			ihpaCr := &autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler{}

			// We'll need to retry getting this newly created CronJob, given that creation may not immediately happen.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, ihpaKey, ihpaCr)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
			// Let's make sure our Schedule string value was properly converted/handled.
			Expect(ihpaCr.ObjectMeta.Name).Should(Equal(testName))

			time.Sleep(10 * time.Second)
		})
	})

})

func CreateIHPAInstance() *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler {
	return &autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
		},
		Spec: autoscalingv1alpha1.IntelligentHorizontalPodAutoscalerSpec{
			ScaleTargetRef: k8sautoscalingv2.CrossVersionObjectReference{
				Kind:       "StatefulSet",
				Name:       testName,
				APIVersion: "apps/v1",
			},
			MinReplicas: 2,
			MaxReplicas: 100,
			Paused:      false,
			ScaleMode:   autoscalingv1alpha1.ScaleModeAuto,
			PortraitProviders: []autoscalingv1alpha1.HorizontalPortraitProvider{
				{
					Type:     autoscalingv1alpha1.CronHorizontalPortraitProviderType,
					Priority: 2,
					Cron: &autoscalingv1alpha1.CronHorizontalPortraitProvider{
						Crons: []autoscalingv1alpha1.ReplicaCron{
							{
								Name:     "cron1",
								TimeZone: "UTC",
								Start:    "29 * * * ?",
								End:      "59 * * * ?",
								Replicas: 5,
							},
						},
					},
				},
				{
					Type:     autoscalingv1alpha1.StaticHorizontalPortraitProviderType,
					Priority: 1,
					Static: &autoscalingv1alpha1.StaticHorizontalPortraitProvider{
						Replicas: 4,
					},
				},
			},
			Behavior: autoscalingv1alpha1.IntelligentHorizontalPodAutoscalerBehavior{
				ScaleDown: autoscalingv1alpha1.ScalingBehavior{
					GrayStrategy: &autoscalingv1alpha1.GrayStrategy{
						GrayState:             autoscalingv1alpha1.PodStateCutoff,
						ChangePercent:         20,
						ChangeIntervalSeconds: 1800,
						ObservationSeconds:    3600,
					},
				},
				ReplicaProfile: &autoscalingv1alpha1.ReplicaProfileBehavior{
					PodSorter: autoscalingv1alpha1.PodSorter{
						Type: autoscalingv1alpha1.WorkloadDefaultPodSorterType,
					},
					PodTrafficController: autoscalingv1alpha1.PodTrafficController{
						Type: autoscalingv1alpha1.ReadinessGatePodTrafficControllerType,
					},
				},
			},
		},
		Status: autoscalingv1alpha1.IntelligentHorizontalPodAutoscalerStatus{
			PreviousPortraitValue: &autoscalingv1alpha1.HorizontalPortraitValue{
				Provider:   string(autoscalingv1alpha1.StaticHorizontalPortraitProviderType),
				Replicas:   10,
				ExpireTime: metav1.Now(),
			},
		},
	}
}
