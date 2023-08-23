package main

import (
	"MutatingWebhook/client"      // 导入自定义的 client 包
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"           // 导入 Kubernetes Admission 控制器的 API 包   
	corev1 "k8s.io/api/core/v1"             // 导入 Kubernetes 核心 API 包
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"        // 导入 Kubernetes 元数据 API 包
	"k8s.io/apimachinery/pkg/runtime"                   // 导入 Kubernetes 运行时包
	"k8s.io/apimachinery/pkg/runtime/serializer"       // 导入 Kubernetes 序列化包
	"log"
	"math"
	"net/http"
	"strconv"
	"time"
)

// PatchOperation 定义了用于 JSON 补丁操作的结构体
type PatchOperation struct {
	Op    string      `json:"op"`                       // 补丁操作的类型，如 "add"、"replace"、"remove"
	Path  string      `json:"path"`                     // 补丁操作的目标路径，表示要修改的 JSON 字段位置
	Value interface{} `json:"value,omitempty"`          // 补丁操作的值，要添加、替换或移除的新值
}

// 用于存储补丁操作
var Patches []PatchOperation              // 切片（slice）类型，用于存储 PatchOperation 结构体的实例。在代码中，Patches 用于收集要应用到 Kubernetes 资源的多个补丁操作。
                                          // 每个补丁操作都描述了对资源的不同修改，比如添加、替换或删除字段。


var (
	scheme = runtime.NewScheme()                          // 创建一个新的运行时方案,用于将 JSON 格式的数据结构与其对应的 Go 类型进行映射。通过 runtime.NewScheme() 创建一个新的运行时方案。
	Codecs = serializer.NewCodecFactory(scheme)           // 创建一个新的序列化编解码工厂,是一个序列化编解码工厂，用于在 API 对象和 JSON 之间进行序列化和反序列化。
	                                                      // 它通过指定的方案（scheme）来确定如何编码和解码对象。通过 serializer.NewCodecFactory(scheme) 创建一个新的序列化编解码工厂，
	                                                      // 该工厂将使用之前创建的运行时方案。
)

func main() {
	// 初始化 glog 配置
	flag.Parse()          // 解析命令行标志，即读取命令行参数并进行处理

	// 设置日志输出选项，这里禁止将日志输出到标准错误
	flag.Set("logtostderr", "false")
	flag.Set("alsologtostderr", "false")

	// 设置日志输出选项，这里禁止将日志输出到标准错误
	flag.Set("log_dir", "/var/log/myapp")

	// 启动 glog 刷新循环，每秒刷新一次日志, 创建了一个匿名的 Go 协程（goroutine），用于周期性地刷新日志并保持日志持久化。
	go func() {
		for {
			glog.Flush()            // 刷新日志，将缓存中的日志写入磁盘
			time.Sleep(1 * time.Second)    // 休眠 1 秒
		}
	}()                 // 可以确保程序的日志在持续运行时定期刷新到磁盘，从而保持日志记录的完整性和持久性。这在长时间运行的应用中是非常有用的，以防止日志缓存过多而导致丢失日志信息。
	
        // 读取证书文件和私钥文件
	cert, err := tls.LoadX509KeyPair("/etc/webhook/certs/tls.crt", "/etc/webhook/certs/tls.key")
	if err != nil {
		glog.Errorf("get cert fail.err is :", err)
		panic(err)       // 如果加载失败，输出错误信息并终止程序
	}
        // 创建 TLS 配置, 创建了一个 tls.Config 结构体，用于配置 HTTPS 服务器的 TLS 设置，包括加载的证书
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},        // 加载的证书
		//ClientCAs:    caCertPool,               // 客户端证书颁发机构的证书池
		//ClientAuth:   tls.RequireAndVerifyClientCert,    // 客户端证书认证设置
	}

	// 创建 HTTP 服务器, 用于监听和处理来自客户端的请求，并使用 TLS 加密进行安全通信。
	server := &http.Server{
		Addr:      ":8443",        // 服务器监听的地址和端口
		TLSConfig: tlsConfig,      // 使用的 TLS 配置,用于配置服务器的 TLS 设置，包括加载的证书和其他 TLS 参数。
	}

	// 处理 /webhook 路径的请求，使用 applyNode 处理器
	http.Handle("/webhook", New(&applyNode{}))       // webhook 路径将被映射到名为 applyNode 的请求处理器, 当客户端访问 /webhook 路径时，会由 applyNode 处理器来处理请求。

	// 创建 Kubernetes 客户端
	client.NewClientK8s()           // 用了自定义的 client 包中的 NewClientK8s() 函数，用于创建一个 Kubernetes 客户端, 以便后续的代码可以使用该客户端与 Kubernetes 集群进行交互


	// 启动了 HTTPS 服务器，开始监听来自客户端的加密请求。如果启动过程中出现错误，会将错误信息记录在日志中，并且使用 panic() 函数终止程序的执行。
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
	
	// 实现 handler 接口，处理 HTTP 请求, 通过调用 webHookVerify 函数来验证和处理请求，并根据处理结果进行相应的响应设置。
	if bytes, err := webHookVerify(w, r); err != nil {
		glog.Errorf("Error handling webhook request: %v", err)        // 记录错误信息到日志
		w.WriteHeader(http.StatusInternalServerError)                // 设置 HTTP 响应状态码为 500
		_, writeErr = w.Write([]byte(err.Error()))                   // 将错误信息写入响应
	} else {
		log.Print("Webhook request handled successfully")          // 记录成功处理的日志信息
		_, writeErr = w.Write(bytes)                             // 记录成功处理的日志信息
		w.Header().Set("Content-Type", "application/json")       // 设置响应头的 Content-Type
		w.WriteHeader(http.StatusOK)       // 设置 HTTP 响应状态码为 200
	}

	// 处理可能的写入错误
	if writeErr != nil {
		glog.Errorf("Could not write response: %v", writeErr)         // 记录错误信息到日志
	}
	return
}

// webHookVerify 验证和处理 Webhook 请求, 定义了一个名为 webHookVerify 的函数，用于验证和处理传入的 HTTP 请求，并解析 AdmissionReview 请求
// 这个函数用于验证请求的合法性，解析 AdmissionReview 请求，并根据不同的条件调用相应的处理函数来进行处理
func webHookVerify(w http.ResponseWriter, r *http.Request) (bytes []byte, err error) {
	// 检查请求方法是否为 POST
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil, fmt.Errorf("invalid method %s, only POST requests are allowed", r.Method)
	}
        // 根据对象类型调用对应的处理函数（这里是 nodePatch 函数）
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
	node := corev1.Node{}
	obj, _, err := Codecs.UniversalDecoder().Decode(admissionReviewReq.Request.Object.Raw, nil, &node)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("Request type is not Node.err is: %v", err)
	}

	// 检查命名空间是否为公共命名空间，如果是则拒绝修改
	if admissionReviewReq.Request.Namespace == metav1.NamespacePublic || admissionReviewReq.Request.Namespace == metav1.NamespaceSystem {
		glog.Infof("ns is a public resource and is prohibited from being modified.ns is :", admissionReviewReq.Request.Namespace)
		return nil, nil
	}
	//nodeInfo, _ := obj.(*corev1.Node)
	//jsonData, err := json.Marshal(nodeInfo)
	//fmt.Println(string(jsonData))
	// 根据对象类型调用对应的处理函数（这里是 nodePatch 函数）
	if _, ok := obj.(*corev1.Node); ok {
		bytes, err = nodePatch(admissionReviewReq, node)
	}

	if err != nil {
		glog.Errorf("node server err,err is:", err)
	}
	return bytes, err
}

// 用于处理节点的修改逻辑，并返回修改后的 AdmissionReview 响应
// 首先查询了节点的 CPU 和内存使用情况。然后，根据计算结果确定了最终的 CPU 和内存值。接着，构建了 AdmissionReview 响应结构，并构建了用于 Patch 操作的列表。
// 最后，将 AdmissionReview 响应序列化为 JSON 格式，返回给调用者
func nodePatch(admissionReviewReq v1beta1.AdmissionReview, nodeInfo corev1.Node) (bytes []byte, err error) {
	var finalCpu string
	var finalMem string

	// 用于处理节点的修改逻辑，并返回修改后的 AdmissionReview 响应
	nodeMetrics, err := client.K8sClient.MetricsApi.MetricsV1beta1().NodeMetricses().Get(context.Background(), nodeInfo.Name, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	//抓取node的capacity和allocatable值
	// capacityCpu := nodeInfo.Status.Capacity.Cpu().MilliValue()       //单位 m，1c = 1000m     describe node得到的capacity.cpu字段值
	// capacityMem := nodeInfo.Status.Capacity.Memory().Value() / (1024 * 1024)   //单位 M       describe node得到的capacity.memory字段值

	allocatableCpu := nodeInfo.Status.Allocatable.Cpu().MilliValue()
	allocatableMem := nodeInfo.Status.Allocatable.Memory().Value()/(1024*1024)
	
	//fmt.Println("capacityCpu cpu is", capacityCpu)       //单位 m，1c = 1000m     describe node得到的capacity.cpu字段值 (数值)
	//fmt.Println("capacityMem mem is", capacityMem)       //单位 M       describe node得到的capacity.memory字段值      (数值)
	
	//fmt.Println("Allocatable string cpu is", nodeInfo.Status.Allocatable.Cpu().String())    //单位 c     describe node得到的allocatable.cpu字段值    (字符串)
	//fmt.Println("Allocatable string mem is", nodeInfo.Status.Allocatable.Memory().String()) //单位 Ki       describe node得到的allocatable.memory字段值   (字符串)
	
	//fmt.Println("Allocatable cpu is", nodeInfo.Status.Allocatable.Cpu().MilliValue())           //单位 m，1c = 1000m     describe node得到的allocatable.cpu字段值    (数值)
	//fmt.Println("Allocatable mem is", nodeInfo.Status.Allocatable.Memory().Value()/(1024*1024))   //单位 M       describe node得到的allocatable.memory字段值         (数值)
	
	//fmt.Println("nodeMetrics cpu is", nodeMetrics.Usage.Cpu().MilliValue())               //单位 m，1c = 1000m    node实际资源使用量
	//fmt.Println("nodeMetrics mem is", nodeMetrics.Usage.Memory().Value()/(1024*1024))     //单位 M                 node实际资源使用量
 
	// 计算节点剩余的 CPU 和内存资源
	// allocatableCpu := capacityCpu - nodeMetrics.Usage.Cpu().MilliValue()
	// allocatableMem := capacityMem - nodeMetrics.Usage.Memory().Value()/(1024*1024)
	availableCpu := allocatableCpu - nodeMetrics.Usage.Cpu().MilliValue()
	availableMem := allocatableMem - nodeMetrics.Usage.Memory().Value()/(1024*1024)

	// 根据计算结果，确定最终的 CPU 和内存值
	if availableCpu > allocatableCpu {
		floatCpu := math.Round(float64(availableCpu / 1000))     //目的是将单位换成c,且将类型换成字符串类型
		finalCpu = strconv.FormatFloat(floatCpu, 'f', -1, 64)
	} else {
		finalCpu = nodeInfo.Status.Allocatable.Cpu().String()    //单位 c     describe node得到的allocatable.cpu字段值    (字符串)
	}
	if availableMem > allocatableMem {
		finalMem = strconv.Itoa(int(availableMem*1024)) + "Ki"   //目的是将单位换成Ki,且将类型换成字符串类型
	} else {
		finalMem = nodeInfo.Status.Allocatable.Memory().String()   //单位 Ki       describe node得到的allocatable.memory字段值   (字符串)
	}

	// 构建 AdmissionReview 响应结构
	admissionReviewResponse := v1beta1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &v1beta1.AdmissionResponse{
			UID: admissionReviewReq.Request.UID,
		},
	}

	// 构建 Patch 操作列表
	//Oversold logic can be added here
	patchOps := append(Patches, getPatchItem("replace", "/status/allocatable/cpu", string(finalCpu)), getPatchItem("replace", "/status/allocatable/memory", finalMem))
	patchBytes, err := json.Marshal(patchOps)
	admissionReviewResponse.Response.Allowed = true
	admissionReviewResponse.Response.Patch = patchBytes
	admissionReviewResponse.Response.PatchType = func() *v1beta1.PatchType {
		pt := v1beta1.PatchTypeJSONPatch
		return &pt
	}()

	// 将 AdmissionReview 响应序列化为 JSON
	// Return the AdmissionReview with a response as JSON.
	bytes, err = json.Marshal(&admissionReviewResponse)
	return
}

// 定义了一个函数 getPatchItem，用于创建一个 PatchOperation 结构体实例，并返回该实例
// 这个函数接受三个参数：op 表示操作类型（例如，"replace" 表示替换操作），path 表示操作的路径，val 表示操作的值。根据这些参数，函数创建了一个 PatchOperation 结构体实例，并返回它
// 这个函数的作用是为了简化创建 PatchOperation 结构体的过程。在其他地方，如果需要创建一个新的 PatchOperation 实例，可以直接调用这个函数，传入相应的参数，从而避免了每次手动创建结构体实例的重复代码
func getPatchItem(op string, path string, val interface{}) PatchOperation {
	return PatchOperation{
		Op:    op,
		Path:  path,
		Value: val,
	}
}

// 定义了一个名为 Handler 的接口类型，其中声明了一个方法 handler，用于处理 HTTP 请求。
// w 是一个 http.ResponseWriter，用于写入 HTTP 响应，r 是一个 http.Request，表示传入的 HTTP 请求。实现了这个接口的类型必须提供对应的 handler 方法，以便处理具体的 HTTP 请求。
type Handler interface {
	handler(w http.ResponseWriter, r *http.Request)
}

// 定义了一个名为 HandleProxy 的结构体类型，用于包装一个实现了 Handler 接口的对象。
// 字段 handler，它的类型是实现了 Handler 接口的对象。通过这种方式，HandleProxy 结构体可以将具体的请求处理逻辑委托给包装的 handler 对象来执行。
type HandleProxy struct {
	handler Handler
}

// 定义了一个函数 New，用于创建一个新的 HandleProxy 实例，并将传入的实现了 Handler 接口的对象作为参数。
// 最后，函数返回创建的 HandleProxy 实例的指针。
// 这个函数的作用是用于创建一个新的 HandleProxy 实例，通过传入不同的 handler 对象，可以实现不同的请求处理逻辑。
func New(handler Handler) *HandleProxy {
	return &HandleProxy{
		handler: handler,
	}
}

// The Handle needs to implement ServeHTTP
// 为 HandleProxy 结构体定义了一个方法 ServeHTTP，该方法是为了实现 http.Handler 接口的要求
// HandleProxy 结构体可以用作 HTTP 处理器
func (h *HandleProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	h.handler.handler(w, r)
}

