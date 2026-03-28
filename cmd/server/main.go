package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openmux/openmux/internal/auth"
	"github.com/openmux/openmux/internal/autoroute"
	"github.com/openmux/openmux/internal/balancer"
	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/discovery"
	"github.com/openmux/openmux/internal/handler"
	"github.com/openmux/openmux/internal/middleware"
	"github.com/openmux/openmux/internal/provider"
	"github.com/openmux/openmux/internal/router"
	"github.com/openmux/openmux/pkg/logger"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	// 设置日志级别
	logger.SetLevel(cfg.Monitoring.LogLevel)

	logger.Infof("Starting OpenMux server on %s:%d", cfg.Server.Host, cfg.Server.Port)

	// Log active providers
	logger.Infof("Active Providers:")
	for name, p := range cfg.Providers {
		logger.Infof("- %s (Type: %s, BaseURL: %s, Keys: %d)", name, p.Type, p.BaseURL, len(p.APIKeys))
	}

	// Log configured model routes
	logger.Infof("Configured Model Routes:")
	for name, route := range cfg.ModelRoutes {
		logger.Infof("- %s: %d targets (Strategy: %s)", name, len(route.Targets), route.Strategy)
		for _, t := range route.Targets {
			logger.Infof("  -> %s/%s (Weight: %d)", t.Provider, t.Model, t.Weight)
		}
	}

	// 初始化组件
	providerPool := provider.InitFromConfig(cfg)
	balancerPool := balancer.InitFromConfig(cfg)
	modelRouter := router.NewRouter(cfg)
	authManager := auth.NewManager(&cfg.Auth)

	// 创建智能路由
	var autoRouter *autoroute.AutoRouter
	if cfg.AutoRoute.Enabled {
		autoRouter = autoroute.New(cfg.AutoRoute)
		logger.Infof("Auto routing enabled (alias: %q, lite: %q, standard: %q, large: %q, reasoning: %q)",
			autoRouter.Alias(), cfg.AutoRoute.Lite, cfg.AutoRoute.Standard, cfg.AutoRoute.Large, cfg.AutoRoute.Reasoning)
	}

	// 创建处理器
	chatHandler := handler.NewChatHandler(modelRouter, providerPool, balancerPool, autoRouter)
	embeddingHandler := handler.NewEmbeddingHandler(modelRouter, providerPool, balancerPool)
	rerankHandler := handler.NewRerankHandler(modelRouter, providerPool, balancerPool)
	modelsHandler := handler.NewModelsHandler(modelRouter)
	healthHandler := handler.NewHealthHandler(balancerPool)

	// 创建中间件
	authMiddleware := auth.NewMiddleware(authManager)

	// 设置路由
	mux := http.NewServeMux()
	
	// OpenAI 兼容端点
	mux.Handle("/v1/chat/completions", 
		middleware.Recovery(
			middleware.Logger(
				middleware.CORS(
					authMiddleware.Authenticate(
						http.HandlerFunc(chatHandler.Handle),
					),
				),
			),
		),
	)

	mux.Handle("/v1/embeddings",
		middleware.Recovery(
			middleware.Logger(
				middleware.CORS(
					authMiddleware.Authenticate(
						http.HandlerFunc(embeddingHandler.Handle),
					),
				),
			),
		),
	)

	mux.Handle("/v1/rerank",
		middleware.Recovery(
			middleware.Logger(
				middleware.CORS(
					authMiddleware.Authenticate(
						http.HandlerFunc(rerankHandler.Handle),
					),
				),
			),
		),
	)
	
	mux.Handle("/v1/models",
		middleware.Recovery(
			middleware.Logger(
				middleware.CORS(
					authMiddleware.Authenticate(
						http.HandlerFunc(modelsHandler.Handle),
					),
				),
			),
		),
	)
	
	// 健康检查端点（无需认证）
	mux.Handle("/health",
		middleware.Recovery(
			middleware.Logger(
				http.HandlerFunc(healthHandler.Handle),
			),
		),
	)

	// 创建服务器
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// 启动模型自动发现
	var disc *discovery.ModelDiscovery
	if cfg.Discovery.Enabled {
		disc = discovery.NewModelDiscovery(cfg, modelRouter)
		go disc.Start(context.Background())
		logger.Infof("Model discovery enabled (interval: %v, providers: %v)", cfg.Discovery.Interval, cfg.Discovery.Providers)
	}

	// 启动服务器
	go func() {
		logger.Infof("Server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Server error: %v", err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Println("Shutting down server...")

	if disc != nil {
		disc.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("Server forced to shutdown: %v", err)
	}

	logger.Println("Server exited")
}
