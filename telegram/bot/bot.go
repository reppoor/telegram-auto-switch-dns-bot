package bot

import (
	"context"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/net/proxy"
	"net"
	"net/http"
	"net/url"
	"strings"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/utils"
	"time"
)

func TelegramApp() {
	cfg := config.Global
	var bot *tgbotapi.BotAPI
	var err error // ✅ 先声明 err
	// 假设 Config.Network.Proxy 是代理地址，Config.Network.EnableProxy 是是否启用代理
	if cfg.Network.EnableProxy {
		utils.Logger.Infof("代理开启使用代理建立telegram连接")
		var httpClient *http.Client
		proxyURL, err := url.Parse(cfg.Network.Proxy)
		if err != nil {
			utils.Logger.Warnf("解析代理地址失败: %v", err)
		}

		// 提取代理用户名和密码
		var proxyAuth *url.Userinfo
		if proxyURL.User != nil {
			proxyAuth = proxyURL.User
		}

		// 判断代理类型，HTTP 或 SOCKS5
		if strings.HasPrefix(proxyURL.Scheme, "http") {
			utils.Logger.Infof("使用http代理建立telegram连接")
			// 如果是 HTTP 代理
			transport := &http.Transport{
				Proxy: func(req *http.Request) (*url.URL, error) {
					// 获取用户名和密码
					username := proxyAuth.Username()
					password, _ := proxyAuth.Password()

					proxyURLWithAuth := &url.URL{
						Scheme: "http",
						Host:   proxyURL.Host,
						User:   url.UserPassword(username, password),
					}
					return proxyURLWithAuth, nil
				},
				DialContext: (&net.Dialer{
					Timeout: 10 * time.Second, // 连接超时
				}).DialContext,
				ResponseHeaderTimeout: 10 * time.Second, // 读取响应头的超时
			}

			// 设置 httpClient 的超时
			httpClient = &http.Client{
				Timeout:   30 * time.Second, // 总超时（连接 + 读取 + 写入）
				Transport: transport,
			}
		} else if strings.HasPrefix(proxyURL.Scheme, "socks5") {
			utils.Logger.Infof("使用socks5代理建立telegram连接")
			// 如果是 SOCKS5 代理
			var dialer proxy.Dialer
			if proxyAuth != nil {
				// 如果 SOCKS5 代理需要认证
				username := proxyAuth.Username()
				password, _ := proxyAuth.Password() // 只取密码部分

				// 创建带认证的 SOCKS5 代理
				dialer, err = proxy.SOCKS5("tcp", proxyURL.Host, &proxy.Auth{
					User:     username,
					Password: password,
				}, proxy.Direct)
				if err != nil {
					utils.Logger.Warnf("连接到 SOCKS5 代理失败: %v", err)
				}
			} else {
				// 如果 SOCKS5 代理不需要认证
				dialer, err = proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
				if err != nil {
					utils.Logger.Warnf("连接到 SOCKS5 代理失败: %v", err)
				}
			}

			// 包装 dialer.Dial 成一个支持 DialContext 的方法
			httpClient = &http.Client{
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
						// 使用代理的 Dial 方法
						return dialer.Dial(network, address)
					},
				},
			}

		} else {
			utils.Logger.Warnf("不支持的代理类型: %s", proxyURL.Scheme)
		}

		// 使用带代理的 httpClient 创建 Telegram Bot
		bot, err = tgbotapi.NewBotAPIWithClient(cfg.Telegram.Token, cfg.Telegram.ApiEndpoint+"/bot%s/%s", httpClient)
		if err != nil {
			utils.Logger.Errorf("使用代理初始化 Telegram Bot 失败: %v", err)

		}
	} else {
		bot, err = tgbotapi.NewBotAPIWithAPIEndpoint(cfg.Telegram.Token, cfg.Telegram.ApiEndpoint+"/bot%s/%s")
		if err != nil {
			utils.Logger.Errorf("直连初始化 Telegram Bot 失败: %v", err)
		}
	}
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60 // 设置超时时间
	//updates := bot.GetUpdatesChan(u) // 获取更新通道
	// 在这里可以安全使用 bot 变量
	utils.Logger.Infof("BOT启动: %s", bot.Self.UserName)
	bot.Debug = true
	InitBot(bot) // 调用初始化
}
