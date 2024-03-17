package main

import (
	"fmt"
	"log"
	"net/http"
	_ "rpc_service/provider/auth/session/memory"
	"rpc_service/provider/auth/session/session"
)

var globalSessions *session.Manager

func init() {
	var err error
	globalSessions, err = session.NewManager("memory", "goSessionid", 3600)
	if err != nil {
		fmt.Println(err)
		return
	}
	go globalSessions.GC()
	fmt.Println("fd")
}

func sayHelloHandler(w http.ResponseWriter, r *http.Request) {

	cookie, err := r.Cookie("goSessionid")
	if err == nil {
		fmt.Println(cookie.Value)
	}
}

func login(w http.ResponseWriter, r *http.Request) {
	sess := globalSessions.SessionStart(w, r)
	val := sess.Get("username")
	if val != nil {
		fmt.Println(val)
	} else {
		sess.Set("username", "jerry")
		fmt.Println("set session")
	}
}

func loginOut(w http.ResponseWriter, r *http.Request) {
	//销毁
	globalSessions.SessionDestroy(w, r)
	fmt.Println("session destroy")
}

func test() {
	http.HandleFunc("/", sayHelloHandler) //	设置访问路由
	http.HandleFunc("/login", login)
	http.HandleFunc("/logout", loginOut) //销毁
	log.Fatal(http.ListenAndServe(":8080", nil))
}
