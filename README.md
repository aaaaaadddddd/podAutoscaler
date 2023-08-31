
## 1. Kubebuilder

地址：https://github.com/kubernetes-sigs/kubebuilder

Kubebuilder 是一个基于 CRD 来构建 Kubernetes API 的框架，可以使用 CRD 来构建 API、Controller 等。

可以理解为类似于进行web开发时，java的springboot框架，go的gin框架等。

这里有一个中文的翻译文档：https://cloudnative.to/kubebuilder/quick-start.html

kubebuilder目前不支持windows，所以我们需要将其安装在linux机器上。


安装kubebuilder

~~~shell
curl -L -o kubebuilder https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)
chmod +x kubebuilder && mv kubebuilder /usr/local/bin/
~~~


## 2. 实际案例

需求：

（1）创建CRD

（2）创建Kind ：PodAutoscaler

（3）创建POD，支持副本数控制，验证

（4）创建多副本POD，支持副本收缩

~~~shell
[root@master code]# cd rediscrd/
[root@master code]# git init
[root@master rediscrd]# git remote add origin git@gitee.com:mszlu/redis-crd.git
[root@master rediscrd]# git pull
[root@master rediscrd]# kubebuilder init --domain test.com --repo test.com/rediscrd
Writing kustomize manifests for you to edit...
Writing scaffold for you to edit...
Get controller runtime:
$ go get sigs.k8s.io/controller-runtime@v0.14.1
Update dependencies:
$ go mod tidy
Next: define a resource with:
$ kubebuilder create api
[root@master rediscrd]# kubebuilder create api --group myapp --version=v1 --kind RedisCRD
Create Resource [y/n]
y
Create Controller [y/n]
y
Writing kustomize manifests for you to edit...
Writing scaffold for you to edit...
api/v1/rediscrd_types.go
controllers/rediscrd_controller.go
Update dependencies:
$ go mod tidy
Running make:
$ make generate
mkdir -p /mnt/go/code/rediscrd/bin
test -s /mnt/go/code/rediscrd/bin/controller-gen && /mnt/go/code/rediscrd/bin/controller-gen --version | grep -q v0.11.1 || \
GOBIN=/mnt/go/code/rediscrd/bin go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.11.1
/mnt/go/code/rediscrd/bin/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
Next: implement your new API and generate the manifests (e.g. CRDs,CRs) with:
$ make manifests
~~~

### 2.1 创建Pod

在controller下新建redis_helper.go

~~~go
package controllers

import (
	"context"
	coreV1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	v1 "test.com/rediscrd/api/v1"
)

func CreateRedis(client client.Client, redisConfig *v1.RedisCRD) error {
	newPod := &coreV1.Pod{}
	newPod.Name = redisConfig.Name
	newPod.Namespace = redisConfig.Namespace
	newPod.Spec.Containers = []coreV1.Container{
		{
			Name:            redisConfig.Name,
			Image:           "redis:5.0.14",
			ImagePullPolicy: coreV1.PullIfNotPresent,
			Ports: []coreV1.ContainerPort{
				{
					ContainerPort: int32(redisConfig.Spec.Port),
				},
			},
		},
	}
	return client.Create(context.Background(), newPod)
}

~~~



~~~go
func (r *RedisCRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here
	redis := &myappv1.RedisCRD{}
	err := r.Get(ctx, req.NamespacedName, redis)
	if err != nil {
		log.Log.Error(err, "get RedisCRD err")
	} else {
		log.Log.Info("redis Obj:", redis)
		err := CreateRedis(r.Client, redis)
		if err != nil {
			log.Log.Info("create pod failure,", err)
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

~~~



这样当我们构建redisCRD的时候，就会新建一个Pod，还需要做一个步骤，删除CRD的时候，也需要删除Pod。

需要使用finalizers。

Finalizer 是带有命名空间的键，告诉 Kubernetes 等到特定的条件被满足后， 再完全删除被标记为删除的资源。 Finalizer 提醒控制器清理被删除的对象拥有的资源。

当你告诉 Kubernetes 删除一个指定了 Finalizer 的对象时， Kubernetes API 通过填充 .metadata.deletionTimestamp 来标记要删除的对象， 并返回202状态码 (HTTP "已接受") 使其进入只读状态。 此时控制平面或其他组件会采取 Finalizer 所定义的行动， 而目标对象仍然处于终止中（Terminating）的状态。 这些行动完成后，控制器会删除目标对象相关的 Finalizer。 当 metadata.finalizers 字段为空时，Kubernetes 认为删除已完成。

~~~go
func (r *RedisCRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Enabled()
	// TODO(user): your logic here
	redis := &myappv1.RedisCRD{}
	err := r.Get(ctx, req.NamespacedName, redis)
	if err != nil {
		logger.Error(err, "get RedisCRD err")
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
	logger.Info("redis Obj:", "obj", redis)
	podName, err := CreateRedis(r.Client, redis)
	if err != nil {
		logger.Info("create pod failure,", "errInfo", err)
		if errors.IsAlreadyExists(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	//创建时加上Finalizers
	redis.Finalizers = append(redis.Finalizers, podName)
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
func (r *RedisCRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&myappv1.RedisCRD{}).
		Complete(r)
}

func (r *RedisCRDReconciler) clearRedis(ctx context.Context, redis *myappv1.RedisCRD) error {
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
			return err
		}
		log.Log.Info("delete success!!")
	}
	//清空finalizers，只要它有值，就无法删除Kind
	redis.Finalizers = []string{}
	return r.Client.Update(ctx, redis)
}

~~~

~~~go

func CreateRedis(client client.Client, redisConfig *v1.RedisCRD) (string, error) {
	newPod := &coreV1.Pod{}
	newPod.Name = redisConfig.Name
	newPod.Namespace = redisConfig.Namespace
	newPod.Spec.Containers = []coreV1.Container{
		{
			Name:            redisConfig.Name,
			Image:           "redis:5.0.14",
			ImagePullPolicy: coreV1.PullIfNotPresent,
			Ports: []coreV1.ContainerPort{
				{
					ContainerPort: int32(redisConfig.Spec.Port),
				},
			},
		},
	}
	return newPod.Name, client.Create(context.Background(), newPod)
}
~~~

进行测试：

~~~shell
apiVersion: myapp.test.com/v1
kind: RedisCRD
metadata:
  labels:
    app.kubernetes.io/name: rediscrd
    app.kubernetes.io/instance: rediscrd-sample
    app.kubernetes.io/part-of: rediscrd
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/created-by: rediscrd
  name: rediscrd-sample
spec:
  # TODO(user): Add fields here
  port: 6379
~~~

~~~shell
make install
make run
kubectl apply -f config/sample
~~~

~~~shell
[root@master rediscrd]# kubectl get redisCRD
NAME              AGE
rediscrd-sample   6s
[root@master rediscrd]# kubectl get pods
NAME              READY   STATUS    RESTARTS   AGE
rediscrd-sample   1/1     Running   0          10s
~~~

~~~shell
[root@master rediscrd]# kubectl delete -f config/samples/
rediscrd.myapp.test.com "rediscrd-sample" deleted
[root@master rediscrd]# kubectl get redisCRD
No resources found in default namespace.
[root@master rediscrd]# kubectl get pods
No resources found in default namespace.
~~~

### 2.2 多副本

~~~go
type RedisCRDSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of RedisCRD. Edit rediscrd_types.go to remove/update
	Foo string `json:"foo,omitempty"`
	// +kubebuilder:validation:Maximum:=6380
	// +kubebuilder:validation:Minimum:=6370
	Port int `json:"port"`
	Num  int `json:"num"`
}
~~~

~~~yml
apiVersion: myapp.test.com/v1
kind: RedisCRD
metadata:
  labels:
    app.kubernetes.io/name: rediscrd
    app.kubernetes.io/instance: rediscrd-sample
    app.kubernetes.io/part-of: rediscrd
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/created-by: rediscrd
  name: rediscrd-sample
spec:
  # TODO(user): Add fields here
  port: 6379
  num: 2

~~~

我们要达到目的，根据设定的num创建pod。



~~~go
num := redis.Spec.Num
	for i := 1; i <= num; i++ {
		podName := fmt.Sprintf("%s-%d", redis.Name, i)
		err := CreateRedis(r.Client, redis, podName)
		if err != nil {
			logger.Info("create pod failure,", "errInfo", err)
			if errors.IsAlreadyExists(err) {
				continue
			}
			return ctrl.Result{}, err
		}
		//创建时加上Finalizers
		redis.Finalizers = append(redis.Finalizers, podName)
	}
~~~

~~~shell
[root@master rediscrd]# kubectl apply -f config/samples/      
rediscrd.myapp.test.com/rediscrd-sample created
[root@master rediscrd]# kubectl get pods
NAME                READY   STATUS    RESTARTS   AGE
rediscrd-sample-1   1/1     Running   0          23s
rediscrd-sample-2   1/1     Running   0          23s
~~~

~~~shell
[root@master rediscrd]# kubectl delete -f config/samples/
rediscrd.myapp.test.com "rediscrd-sample" deleted
[root@master rediscrd]# kubectl get pods
No resources found in default namespace.
~~~

### 2.3 自动收缩

~~~go
func (r *RedisCRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Enabled()
	// TODO(user): your logic here
	redis := &myappv1.RedisCRD{}
	err := r.Get(ctx, req.NamespacedName, redis)
	if err != nil {
		logger.Error(err, "get RedisCRD err")
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
		err := CreateRedis(r.Client, redis, podName)
		if err != nil {
			logger.Info("create pod failure,", "errInfo", err)
			if errors.IsAlreadyExists(err) {
				continue
			}
			return ctrl.Result{}, err
		}
		//创建时加上Finalizers
		redis.Finalizers = append(redis.Finalizers, podName)
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
func (r *RedisCRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&myappv1.RedisCRD{}).
		Complete(r)
}

func (r *RedisCRDReconciler) clearRedis(ctx context.Context, redis *myappv1.RedisCRD) error {
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
			return err
		}
		log.Log.Info("delete success!!")
	}
	//清空finalizers，只要它有值，就无法删除Kind
	redis.Finalizers = []string{}
	return r.Client.Update(ctx, redis)
}

func (r *RedisCRDReconciler) clearRedisUpdateNum(ctx context.Context, redis *myappv1.RedisCRD) error {
	num := redis.Spec.Num
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
~~~

### 2.4 删除后重建

如果pod被手动删除，我们做的这个是达不到deployment的效果的，所以我们要加一个监听，在删除后判断，然后重建对应的pod

~~~go
func (r *RedisCRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&myappv1.RedisCRD{}).
		Watches(&source.Kind{Type: &v1.Pod{}}, handler.Funcs{
			DeleteFunc: r.podDeleteHandler,
		}).
		Complete(r)
}
~~~

这有一个链接：https://kubernetes.io/blog/2021/05/14/using-finalizers-to-control-deletion/

~~~go
func isExistPod(podName string, redis *v1.RedisCRD, client client.Client) bool {
	err := client.Get(context.Background(), types.NamespacedName{
		Name:      podName,
		Namespace: redis.Namespace,
	}, &coreV1.Pod{})

	if err != nil {
		return false
	}
	return true
}
func CreateRedis(client client.Client, redisConfig *v1.RedisCRD, podName string, scheme *runtime.Scheme) error {
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
~~~



~~~go
func (r *RedisCRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Enabled()
	// TODO(user): your logic here
	redis := &myappv1.RedisCRD{}
	err := r.Get(ctx, req.NamespacedName, redis)
	if err != nil {
		logger.Error(err, "get RedisCRD err")
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
func (r *RedisCRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&myappv1.RedisCRD{}).
		Watches(&source.Kind{Type: &v1.Pod{}}, handler.Funcs{
			DeleteFunc: r.podDeleteHandler,
		}).
		Complete(r)
}

func (r *RedisCRDReconciler) clearRedis(ctx context.Context, redis *myappv1.RedisCRD) error {
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

func (r *RedisCRDReconciler) clearRedisUpdateNum(ctx context.Context, redis *myappv1.RedisCRD) error {
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

func (r *RedisCRDReconciler) podDeleteHandler(event event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {
	log.Log.Info("delete pod:", "podName", event.Object.GetName())
	references := event.Object.GetOwnerReferences()
	for _, v := range references {
		if v.Kind == "RedisCRD" && v.Name == "rediscrd-sample" {
			//需要删除重建
			limitingInterface.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: event.Object.GetNamespace(),
					Name:      v.Name,
				}})
		}
	}
}
~~~

## 4. 部署Operator

上面我们开发的controller，也被称之为Operator，我们只需要将开发的operator打包成镜像，并且进行部署，后续通过CRD就可以便捷的实现相应的功能。

kubebuilder生成的makefile中提供了几个命令：

~~~shell
make docker-build IMG=xxx
make docker-push IMG=xxx
make deploy IMG=XXX
~~~

修改一下Dockerfile，加上go代理

~~~dockerfile
# Build the manager binary
FROM golang:1.19 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
ENV GOPROXY=https://goproxy.cn,direct
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM registry.cn-hangzhou.aliyuncs.com/mszlu/gcr.io_distroless_static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]

~~~

~~~shell
docker build -t  registry.cn-hangzhou.aliyuncs.com/mszlu/guestbook:1.0 .
docker push registry.cn-hangzhou.aliyuncs.com/mszlu/guestbook:1.0

docker build -t  registry.cn-hangzhou.aliyuncs.com/mszlu/redis-crd:1.0 .
docker push registry.cn-hangzhou.aliyuncs.com/mszlu/redis-crd:1.0
~~~

~~~shell
make manifests
make deploy IMG=registry.cn-hangzhou.aliyuncs.com/mszlu/guestbook:1.0
make deploy IMG=registry.cn-hangzhou.aliyuncs.com/mszlu/redis-crd:1.0
[root@master rediscrd]# kubectl get pods -n guestbook-system
NAME                                           READY   STATUS    RESTARTS   AGE
guestbook-controller-manager-8494c8dbb-tdwv8   2/2     Running   0          72s
~~~

