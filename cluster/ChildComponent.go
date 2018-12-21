package Cluster

import (
	"fmt"
	"github.com/zllangct/RockGO/component"
	"github.com/zllangct/RockGO/configComponent"
	"github.com/zllangct/RockGO/logger"
	"github.com/zllangct/RockGO/rpc"
	"reflect"
	"strings"
	"sync"
	"time"
)

type ChildComponent struct {
	Component.Base
	locker          sync.RWMutex
	localAddr		string
	rpcMaster       *rpc.TcpClient //master节点
	nodeComponent   *NodeComponent
	reportCollecter []func() (string, float32)
	close   bool
}

func (this *ChildComponent) GetRequire() map[*Component.Object][]reflect.Type {
	requires := make(map[*Component.Object][]reflect.Type)
	requires[this.Parent.Root()] = []reflect.Type{
		reflect.TypeOf(&Config.ConfigComponent{}),
		reflect.TypeOf(&NodeComponent{}),
	}
	return requires
}

func (this *ChildComponent) Awake() error {
	err := this.Parent.Root().Find(&this.nodeComponent)
	if err != nil {
		return err
	}

	go this.ConnectToMaster()
	go this.DoReport()
	return nil
}

func (this *ChildComponent) Destroy() error {
	this.locker.Lock()
	defer this.locker.Unlock()

	this.close =true
	this.ReportClose(this.localAddr)
	return nil
}

//上报节点信息
func (this *ChildComponent) DoReport() {
	this.When(time.Second,
	func() bool {
		return this.rpcMaster!=nil
	},
	func() bool {
		return this.localAddr!=""
	})
	args := &NodeInfo{
		Address: this.localAddr,
		Role:    Config.Config.ClusterConfig.Role,
		AppName: Config.Config.ClusterConfig.AppName,
	}
	var reply bool
	var interval = time.Duration(Config.Config.ClusterConfig.ReportInterval)
	for {
		reply = false
		this.locker.RLock()
		m := make(map[string]float32)
		for _, collector := range this.reportCollecter {
			f, d := collector()
			m[f] = d
		}
		args.Info = m
		this.locker.RUnlock()
		if this.rpcMaster != nil {
			_ = this.rpcMaster.Call("MasterService.ReportNodeInfo", args, &reply)
		}
		time.Sleep(time.Millisecond * interval)
	}
}

//增加上报信息
func (this *ChildComponent) AddReportInfo(field string, collectFunction func() (string, float32)) {
	this.locker.Lock()
	this.reportCollecter = append(this.reportCollecter, collectFunction)
	this.locker.Unlock()
}

//增加上报节点关闭
func (this *ChildComponent) ReportClose(addr string) {
	var reply bool
	if this.rpcMaster != nil {
		_ = this.rpcMaster.Call("MasterService.ReportNodeClose", addr, &reply)
	}
}

//连接到master
func (this *ChildComponent) ConnectToMaster() {
	addr := Config.Config.ClusterConfig.MasterAddress
	callback := func(event string, data ...interface{}) {
		switch event {
		case "close":
			this.OnDropped()
		}
	}
	logger.Info(" Looking for master ......")
	var err error
	for {
		this.rpcMaster, err = this.nodeComponent.ConnectToNode(addr, callback)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond * 500)
	}
	ip:=strings.Split(this.rpcMaster.LocalAddr(),":")[0]
	port:=strings.Split(Config.Config.ClusterConfig.LocalAddress,":")[1]
	this.localAddr =fmt.Sprintf("%s:%s",ip,port)
	this.nodeComponent.Locker().Lock()
	this.nodeComponent.isOnline = true
	this.nodeComponent.Locker().Unlock()

	logger.Info(fmt.Sprintf("Connected to master [ %s ]", addr))
}

//当节点掉线
func (this *ChildComponent) OnDropped() {
	//重新连接 time.Now().Format("2006-01-02T 15:04:05")
	this.locker.RLock()
	defer this.locker.RUnlock()

	if this.close {
		return
	}

	this.nodeComponent.Locker().Lock()
	this.nodeComponent.isOnline = false
	this.nodeComponent.Locker().Unlock()

	logger.Info("Disconnected from master")
	this.ConnectToMaster()
}