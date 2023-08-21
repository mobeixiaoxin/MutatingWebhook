package client

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
	"sync"
)


// Client 结构定义了一个 Kubernetes 客户端的结构体
type Client struct {
	Api        *kubernetes.Clientset          // Client 结构定义了一个 Kubernetes 客户端的结构体
	MetricsApi *versioned.Clientset           // 用于操作 Kubernetes Metrics API 的客户端
}

var K8sClient *Client                 // 一个全局的 Kubernetes 客户端实例
var onceK8s sync.Once                 // 用于确保创建客户端的代码只会被执行一次

//原生创建client，NewClientK8s 创建一个新的 Kubernetes 客户端实例
func NewClientK8s() {
	onceK8s.Do(func() {
		//本地配置信息，从本地配置文件中获取配置信息来创建 Kubernetes 客户端
		cfg, err := clientcmd.BuildConfigFromFlags("", "/etc/config")
		if err != nil {
			glog.Errorf("kubernetes client failed")
			panic(err.Error())
		}
		K8sClient = &Client{Api: nil, MetricsApi: nil}                      // 初始化客户端实例
		//K8sClient.Api, err = kubernetes.NewForConfig(cfg)
		// 使用配置创建 Kubernetes Metrics API 客户端
		K8sClient.MetricsApi, err = versioned.NewForConfig(cfg)
		fmt.Println("k8s service success")
		if err != nil {
			glog.Errorf("kubernetes client failed")
			panic(err.Error())
		}
	})
}
