package main

import (
	"MutatingWebhook/client"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"
)

// PatchOperation 定义了用于 JSON 补丁操作的结构体
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// 用于存储补丁操作
var Patches []PatchOperation


var (
	scheme = runtime.NewScheme()
	Codecs = serializer.NewCodecFactory(scheme)
)

func main() {
	// 初始化 glog 配置
	//Initializing the glog configuration
	flag.Parse()
	flag.Set("logtostderr", "false")
	flag.Set("alsologtostderr", "false")
	flag.Set("log_dir", "/var/log/myapp")

	// 启动 glog 刷新循环，每秒刷新一次日志
	go func() {
		for {
			glog.Flush()
			time.Sleep(1 * time.Second) // Log refreshed every 1 second
		}
	}()

	
        // 读取证书文件和私钥文件
	// Read certificate file and private key file
	cert, err := tls.LoadX509KeyPair("/etc/webhook/certs/tls.crt", "/etc/webhook/certs/tls.key")
	if err != nil {
		glog.Errorf("get cert fail.err is :", err)
		panic(err)
	}
        // 创建 TLS 配置
	// Creating a TLS Configuration
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		//ClientCAs:    caCertPool,
		//ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	// 创建 HTTP 服务器
	// Create an HTTP server
	server := &http.Server{
		Addr:      ":8443",
		TLSConfig: tlsConfig,
	}

	// 处理 /webhook 路径的请求，使用 applyNode 处理器
	// start services
	http.Handle("/webhook", New(&applyNode{}))

	// 创建 Kubernetes 客户端
	client.NewClientK8s()

	// 创建 Kubernetes 客户端
	if err := server.ListenAndServeTLS("", ""); err != nil {
		glog.Errorf("server start fail,err is:", err)
		panic(err)
	}
}

// applyNode 定义了应用节点的处理器
type applyNode struct {
}

// 实现 handler 接口，处理 HTTP 请求
func (ch *applyNode) handler(w http.ResponseWriter, r *http.Request) {
	var writeErr error
	
	// 实现 handler 接口，处理 HTTP 请求
	if bytes, err := webHookVerify(w, r); err != nil {
		glog.Errorf("Error handling webhook request: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_, writeErr = w.Write([]byte(err.Error()))
	} else {
		log.Print("Webhook request handled successfully")
		_, writeErr = w.Write(bytes)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}

	// 处理可能的写入错误
	if writeErr != nil {
		glog.Errorf("Could not write response: %v", writeErr)
	}
	return
}

// webHookVerify 验证和处理 Webhook 请求
func webHookVerify(w http.ResponseWriter, r *http.Request) (bytes []byte, err error) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil, fmt.Errorf("invalid method %s, only POST requests are allowed", r.Method)
	}

	if contentType := r.Header.Get("Content-Type"); contentType != `application/json` {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("unsupported content type %s, only %s is supported", contentType, `application/json`)
	}

	// 解析 AdmissionReview 请求
	var admissionReviewReq v1beta1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&admissionReviewReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("r.Body parsing failed: %v", err)
	} else if admissionReviewReq.Request == nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, errors.New("request is nil")
	}
	glog.Infof("The structure information received by http is :", admissionReviewReq)
	//jsonData, err := json.Marshal(admissionReviewReq)
	//fmt.Println(string(jsonData))

	// 从 AdmissionReview 请求中解码对象，这里假设是 Node 对象
	//You can add multiple services here, if you are modifying a node, go to the server of the node, if it is a pod you can go to the server of the pod
	node := corev1.Node{}
	obj, _, err := Codecs.UniversalDecoder().Decode(admissionReviewReq.Request.Object.Raw, nil, &node)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("Request type is not Node.err is: %v", err)
	}

	if admissionReviewReq.Request.Namespace == metav1.NamespacePublic || admissionReviewReq.Request.Namespace == metav1.NamespaceSystem {
		glog.Infof("ns is a public resource and is prohibited from being modified.ns is :", admissionReviewReq.Request.Namespace)
		return nil, nil
	}
	//nodeInfo, _ := obj.(*corev1.Node)
	//jsonData, err := json.Marshal(nodeInfo)
	//fmt.Println(string(jsonData))
	if _, ok := obj.(*corev1.Node); ok {
		bytes, err = nodePatch(admissionReviewReq, node)
	}

	if err != nil {
		glog.Errorf("node server err,err is:", err)
	}
	return bytes, err
}

func nodePatch(admissionReviewReq v1beta1.AdmissionReview, nodeInfo corev1.Node) (bytes []byte, err error) {
	var finalCpu string
	var finalMem string

	// Query the CPU usage and memory usage of a node
	nodeMetrics, err := client.K8sClient.MetricsApi.MetricsV1beta1().NodeMetricses().Get(context.Background(), nodeInfo.Name, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	capacityCpu := nodeInfo.Status.Capacity.Cpu().MilliValue()
	capacityMem := nodeInfo.Status.Capacity.Memory().Value() / (1024 * 1024)
	//fmt.Println("capacityCpu cpu is", capacityCpu)
	//fmt.Println("capacityMem mem is", capacityMem)
	//fmt.Println("Allocatable cpu is", nodeInfo.Status.Allocatable.Cpu().MilliValue())
	//fmt.Println("Allocatable string cpu is", nodeInfo.Status.Allocatable.Cpu().String())
	//fmt.Println("Allocatable string mem is", nodeInfo.Status.Allocatable.Memory().String())
	//fmt.Println("Allocatable mem is", nodeInfo.Status.Allocatable.Memory().Value()/(1024*1024))
	//fmt.Println("nodeMetrics cpu is", nodeMetrics.Usage.Cpu().MilliValue())
	//fmt.Println("nodeMetrics mem is", nodeMetrics.Usage.Memory().Value()/(1024*1024))
	allocatableCpu := capacityCpu - nodeMetrics.Usage.Cpu().MilliValue()
	allocatableMem := capacityMem - nodeMetrics.Usage.Memory().Value()/(1024*1024)

	if allocatableCpu > nodeInfo.Status.Allocatable.Cpu().MilliValue() {
		floatCpu := math.Round(float64(allocatableCpu / 1000))
		finalCpu = strconv.FormatFloat(floatCpu, 'f', -1, 64)
	} else {
		finalCpu = nodeInfo.Status.Allocatable.Cpu().String()
	}
	if allocatableMem > nodeInfo.Status.Allocatable.Memory().Value()/(1024*1024) {
		finalMem = strconv.Itoa(int(allocatableMem*1024)) + "Ki"
	} else {
		finalMem = nodeInfo.Status.Allocatable.Memory().String()
	}
	admissionReviewResponse := v1beta1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &v1beta1.AdmissionResponse{
			UID: admissionReviewReq.Request.UID,
		},
	}
	//Oversold logic can be added here
	patchOps := append(Patches, getPatchItem("replace", "/status/allocatable/cpu", string(finalCpu)), getPatchItem("replace", "/status/allocatable/memory", finalMem))
	patchBytes, err := json.Marshal(patchOps)
	admissionReviewResponse.Response.Allowed = true
	admissionReviewResponse.Response.Patch = patchBytes
	admissionReviewResponse.Response.PatchType = func() *v1beta1.PatchType {
		pt := v1beta1.PatchTypeJSONPatch
		return &pt
	}()

	// Return the AdmissionReview with a response as JSON.
	bytes, err = json.Marshal(&admissionReviewResponse)
	return
}

func getPatchItem(op string, path string, val interface{}) PatchOperation {
	return PatchOperation{
		Op:    op,
		Path:  path,
		Value: val,
	}
}

type Handler interface {
	handler(w http.ResponseWriter, r *http.Request)
}

type HandleProxy struct {
	handler Handler
}

func New(handler Handler) *HandleProxy {
	return &HandleProxy{
		handler: handler,
	}
}

//The Handle needs to implement ServeHTTP
func (h *HandleProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	h.handler.handler(w, r)
}

