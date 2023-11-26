package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"service_discovery/api"
	"service_discovery/global"
	"service_discovery/model"
	"syscall"
	"time"
)

func main() {
	//init config
	c := flag.String("c", "", "config file path")
	flag.Parse()
	config, err := global.LoadConfig(*c)
	if err != nil {
		log.Println("load config error:", err)
		return
	}
	connOption := new(model.ConnOption)

	//global discovery
	global.Discovery = model.NewDiscovery(config, connOption)

	//init router and start serverDiscovery.CancelSelf()
	router := api.InitRouter()
	srv := &http.Server{
		Addr:    config.HttpServer,
		Handler: router,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen:%s\n", err)
		}
	}()

	//动态监听配置文件变化
	go model.MonitorTheConfiguration(*c, global.Discovery)

	//graceful restart
	quit := make(chan os.Signal)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	<-quit
	log.Println("shutdown discovery server...")
	//cancel
	global.Discovery.CancelSelf()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("server shutdown error:", err)
	}
	select {
	case <-ctx.Done():
		log.Println("timeout of 5 seconds")
	}
	log.Println("server exiting")
}
