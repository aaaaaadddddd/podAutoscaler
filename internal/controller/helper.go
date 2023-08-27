package controller

import (
	"context"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	v1 "my.domain/podAutoscaler/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func isExistPod(podName string, redis *v1.PodAutoscaler, client client.Client) bool {
	err := client.Get(context.Background(), types.NamespacedName{
		Name:      podName,
		Namespace: redis.Namespace,
	}, &coreV1.Pod{})

	if err != nil {
		return false
	}
	return true
}
func CreateRedis(client client.Client, redisConfig *v1.PodAutoscaler, podName string, scheme *runtime.Scheme) error {
	if isExistPod(podName, redisConfig, client) {
		return nil
	}
	newPod := &coreV1.Pod{}
	newPod.Name = podName
	newPod.Namespace = redisConfig.Namespace
	newPod.Spec.Containers = []coreV1.Container{
		{
			Name:            podName,
			Image:           "redis:5.0.14",
			ImagePullPolicy: coreV1.PullIfNotPresent,
			Ports: []coreV1.ContainerPort{
				{
					ContainerPort: int32(redisConfig.Spec.Port),
				},
			},
		},
	}
	controllerutil.SetControllerReference(redisConfig, newPod, scheme)
	err := client.Create(context.Background(), newPod)
	if err != nil {
		redisConfig.Finalizers = append(redisConfig.Finalizers, podName)
	}
	return err
}
