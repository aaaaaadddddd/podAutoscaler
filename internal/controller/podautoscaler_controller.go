/*
Copyright 2023.

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

package controller

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	myappv1 "my.domain/podAutoscaler/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// PodAutoscalerReconciler reconciles a PodAutoscaler object
type PodAutoscalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=webapp.my.domain,resources=podautoscalers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=webapp.my.domain,resources=podautoscalers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=webapp.my.domain,resources=podautoscalers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PodAutoscaler object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *PodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Enabled()
	redis := &myappv1.PodAutoscaler{}
	err := r.Get(ctx, req.NamespacedName, redis)
	if err != nil {
		logger.Error(err, "get PodAutoscaler err")
		return ctrl.Result{}, nil
	}
	//删除POD，删除Kind:RedisCRD过程中，会自动加上DeletionTimestamp字段，据此判断是否删除了自定义资源
	if !redis.DeletionTimestamp.IsZero() {
		err := r.clearRedis(ctx, redis)
		if err != nil {
			logger.Error(err, "delete update err")
		}
		return ctrl.Result{}, err
	}
	//如果更新了num 小于Finalizers数量 删除pod 达到此数量
	if len(redis.Finalizers) > redis.Spec.Num {
		err := r.clearRedisUpdateNum(ctx, redis)
		if err != nil {
			logger.Error(err, "delete update err")
		}
		return ctrl.Result{}, err
	}
	logger.Info("redis Obj:", "obj", redis)
	num := redis.Spec.Num
	for i := 1; i <= num; i++ {
		podName := fmt.Sprintf("%s-%d", redis.Name, i)
		err := CreateRedis(r.Client, redis, podName, r.Scheme)
		if err != nil {
			logger.Info("create pod failure,", "errInfo", err)
			if errors.IsAlreadyExists(err) {
				continue
			}
			return ctrl.Result{}, err
		}
		//创建时加上Finalizers
		//for _, v := range redis.Finalizers {
		//	if v == podName {
		//		continue
		//	}
		//}
		//redis.Finalizers = append(redis.Finalizers, podName)
	}
	//更新状态
	err = r.Client.Update(ctx, redis)
	if err != nil {
		logger.Error(err, "Update err")
		return ctrl.Result{}, err
	}
	logger.Info("call result......")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&myappv1.PodAutoscaler{}).
		Watches(nil, handler.Funcs{
			DeleteFunc: r.podDeleteHandler,
		}).
		Complete(r)
}

func (r *PodAutoscalerReconciler) clearRedis(ctx context.Context, redis *myappv1.PodAutoscaler) error {
	//从finalizers中取出podName，然后执行删除
	for _, finalizer := range redis.Finalizers {
		//删除pod
		err := r.Client.Delete(ctx, &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      finalizer,
				Namespace: redis.Namespace,
			},
		})
		if err != nil {
			log.Log.Error(err, "delete error")
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
		log.Log.Info("delete success!!")
	}
	//清空finalizers，只要它有值，就无法删除Kind
	redis.Finalizers = []string{}
	return r.Client.Update(ctx, redis)
}

func (r *PodAutoscalerReconciler) clearRedisUpdateNum(ctx context.Context, redis *myappv1.PodAutoscaler) error {
	num := redis.Spec.Num
	fmt.Println("----------clearRedisUpdateNum")
	fmt.Println(len(redis.Finalizers))
	if len(redis.Finalizers) <= 0 {
		return nil
	}
	var err error
	for i := len(redis.Finalizers); i > num; i-- {
		//删除pod
		err = r.Client.Delete(ctx, &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      redis.Finalizers[i-1],
				Namespace: redis.Namespace,
			},
		})
		redis.Finalizers = redis.Finalizers[:i-1]
	}
	if err != nil {
		log.Log.Error(err, "delete error")
		return err
	}
	return r.Client.Update(ctx, redis)
}

func (r *PodAutoscalerReconciler) podDeleteHandler(ctx context.Context, event event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {
	log.Log.Info("delete pod:", "podName", event.Object.GetName())
	references := event.Object.GetOwnerReferences()
	for _, v := range references {
		if v.Kind == "PodAutoscaler" && v.Name == "rediscrd-sample" {
			//需要删除重建
			limitingInterface.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: event.Object.GetNamespace(),
					Name:      v.Name,
				}})
		}
	}
}
