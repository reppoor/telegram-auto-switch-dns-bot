package CheckBackend

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net"
	"net/http"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/middleware"
	"telegram-auto-switch-dns-bot/utils"
	"time"
)

// TCPCheckRequest 请求参数
type TCPCheckRequest struct {
	Target string `json:"target" binding:"required"`
	Port   int    `json:"port" binding:"required"`
	Key    string `json:"key" binding:"required"`
}

// TCPCheckResponse 响应结果
type TCPCheckResponse struct {
	Result          bool   `json:"result"`            // true / false
	Target          string `json:"target"`            // 检测目标
	TargetIp        string `json:"target_ip"`         // 检测目标ip
	Message         string `json:"message"`           // 检测返回的消息
	BackendPublicIP string `json:"backend_public_ip"` // 本机公网 IP
}

// APIResponse 统一 REST API 返回结构
type APIResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data,omitempty"`
}

func CheckApi() {
	utils.Logger.Infof("检测后端启动")
	r := gin.Default()
	gin.SetMode(gin.ReleaseMode)
	r.POST("/api/v1/tcp_checks", tcpCheckHandler)
	r.POST("/api/v1/resolve_ip", resolveIPHandler) // 新增：只解析 IP 的接口
	srv := &http.Server{
		Addr:           ":" + config.Global.BackendListen.Port,
		Handler:        r,
		ReadTimeout:    config.Global.BackendListen.ReadTimeout * time.Second,
		WriteTimeout:   config.Global.BackendListen.WriteTimeout * time.Second,
		MaxHeaderBytes: config.Global.BackendListen.MaxHeaderBytes,
	}

	utils.Logger.Infof("检测后端正在监听端口: %s", config.Global.BackendListen.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		utils.Logger.Error("检测后端启动失败:", err)
	}
}

// ----------------- Handler -----------------
func tcpCheckHandler(c *gin.Context) {
	var req TCPCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Logger.Error("WebSocket检测端绑定JSON请求体错误:", err)
		c.JSON(http.StatusBadRequest, APIResponse[any]{Code: 400, Message: "请求参数错误：" + err.Error()})
		return
	}

	utils.Logger.Infof("请求体数据: %+v", req)

	// 校验通信密钥
	if !middleware.ValidateBackendKey(req.Key, c) {
		return
	}

	// 获取目标 IP，如果解析失败，直接返回接口
	var targetIP string
	var Message string
	targetIPs, err := net.LookupIP(req.Target)
	if err == nil && len(targetIPs) > 0 {
		targetIP = targetIPs[0].String()
		Message = ""
	} else {
		utils.Logger.Warnf("⚠️ 无法解析目标 %s 的 IP, 错误消息: %v", req.Target, err)
		targetIP = ""
		Message = fmt.Sprintf("无法解析目标 %s的IP, 错误消息: %v", req.Target, err)

		// 获取本机公网 IP
		backendPublicIP := getPublicIP()

		// 直接返回，不再进行 TCP 检测
		c.JSON(http.StatusOK, APIResponse[TCPCheckResponse]{
			Code:    0,
			Message: "success",
			Data: TCPCheckResponse{
				Result:          false,
				Target:          req.Target,
				TargetIp:        targetIP,
				Message:         Message,
				BackendPublicIP: backendPublicIP,
			},
		})
		return
	}

	// TCP 连接检测，最多尝试 5 次，并发送进度消息
	result := false
	maxTry := 5
	addr := fmt.Sprintf("%s:%d", targetIP, req.Port)

	// 创建一个通道来发送进度消息
	progressChan := make(chan map[string]interface{}, 5)
	doneChan := make(chan bool)
	errorChan := make(chan error)

	// 启动goroutine处理TCP检测
	go func() {
		defer close(progressChan)
		defer close(doneChan)
		defer close(errorChan)

		for i := 1; i <= maxTry; i++ {
			utils.Logger.Infof("🔍 正在检测第 %d/%d 次连接：%s ...", i, maxTry, addr)

			// 发送进度消息
			progressChan <- map[string]interface{}{
				"current": i,
				"total":   maxTry,
				"target":  req.Target,
				"address": addr,
			}

			conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
			if err == nil {
				conn.Close()
				result = true
				utils.Logger.Infof("✅ 检测成功：目标 %s 可访问", addr)
				doneChan <- true
				return
			} else {
				utils.Logger.Warnf("⚠️ 第 %d 次检测失败：%v", i, err)
				if i == maxTry {
					doneChan <- false
					errorChan <- err
					return
				}
			}
		}
		doneChan <- result
	}()

	// 流式发送响应
	c.Stream(func(w io.Writer) bool {
		select {
		case progress, ok := <-progressChan:
			if !ok {
				return false
			}
			// 发送进度消息
			progressResp := APIResponse[map[string]interface{}]{
				Code:    1, // Code=1 表示进度消息
				Message: "progress",
				Data:    progress,
			}
			respBytes, _ := json.Marshal(progressResp)
			w.Write(respBytes)
			w.Write([]byte("\n"))
			return true
		case done := <-doneChan:
			if done {
				Message = ""
			} else {
				err := <-errorChan
				Message = fmt.Sprintf("检测结束,目标 %s无法连接: %v", addr, err)
				utils.Logger.Warnf("❌ 检测结束：目标 %s 无法连接: %v", addr, err)
			}

			// 获取本机公网 IP
			backendPublicIP := getPublicIP()

			// 发送最终结果
			finalResp := APIResponse[TCPCheckResponse]{
				Code:    0,
				Message: "success",
				Data: TCPCheckResponse{
					Result:          result,
					Target:          req.Target,
					TargetIp:        targetIP,
					Message:         Message,
					BackendPublicIP: backendPublicIP,
				},
			}
			respBytes, _ := json.Marshal(finalResp)
			w.Write(respBytes)
			w.Write([]byte("\n"))
			return false
		}
	})
}

// resolveIPHandler 只解析域名获取 IP，不进行连通性检测
func resolveIPHandler(c *gin.Context) {
	var req TCPCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Logger.Error("解析IP接口绑定JSON请求体错误:", err)
		c.JSON(http.StatusBadRequest, APIResponse[any]{Code: 400, Message: "请求参数错误：" + err.Error()})
		return
	}

	utils.Logger.Infof("解析IP请求体数据: %+v", req)

	// 校验通信密钥
	if !middleware.ValidateBackendKey(req.Key, c) {
		return
	}

	// 获取目标 IP
	var targetIP string
	var Message string
	targetIPs, err := net.LookupIP(req.Target)
	if err == nil && len(targetIPs) > 0 {
		targetIP = targetIPs[0].String()
		Message = ""
		utils.Logger.Infof("✅ 成功解析 %s 的 IP: %s", req.Target, targetIP)
	} else {
		utils.Logger.Warnf("⚠️ 无法解析目标 %s 的 IP, 错误消息: %v", req.Target, err)
		targetIP = ""
		Message = fmt.Sprintf("无法解析目标 %s的IP, 错误消息: %v", req.Target, err)
	}

	// 获取本机公网 IP
	backendPublicIP := getPublicIP()

	// 直接返回解析结果，不进行 TCP 连通性检测
	c.JSON(http.StatusOK, APIResponse[TCPCheckResponse]{
		Code:    0,
		Message: "success",
		Data: TCPCheckResponse{
			Result:          false, // 未进行连通性检测，始终为 false
			Target:          req.Target,
			TargetIp:        targetIP,
			Message:         Message,
			BackendPublicIP: backendPublicIP,
		},
	})
}

// ----------------- 获取公网 IP -----------------
func getPublicIP() string {
	client := http.Client{
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("https://ipinfo.io/json")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var data struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	return data.IP
}
