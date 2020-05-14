package process

import (
	"archive/zip"
	"github.com/infinit-lab/taiji/src/model/base"
	"github.com/infinit-lab/taiji/src/model/process"
	"github.com/infinit-lab/yolanda/bus"
	"github.com/infinit-lab/yolanda/config"
	"github.com/infinit-lab/yolanda/httpserver"
	"github.com/infinit-lab/yolanda/logutils"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var m *manager
var ph *processHandler
var g *guard
var s *slave

type updateManager struct {
	mutex sync.Mutex
	updates map[int]string
}

var um *updateManager

func init() {
	if config.GetBool("process.guard") {
		s = new(slave)
		s.run()
	} else {
		m = new(manager)
		m.run()

		ph = new(processHandler)
		ph.m = m
		bus.Subscribe(base.KeyProcess, ph)
		bus.Subscribe(base.KeyProcessStatus, ph)

		httpserver.RegisterHttpHandlerFunc(http.MethodGet, "/api/1/process", HandleGetProcessList1, true)
		httpserver.RegisterHttpHandlerFunc(http.MethodGet, "/api/1/process/+", HandleGetProcess1, true)
		httpserver.RegisterHttpHandlerFunc(http.MethodPut, "/api/1/process/+/operation", HandlePutProcessOperation1, true)
		httpserver.RegisterHttpHandlerFunc(http.MethodGet, "/api/1/process/+/status", HandleGetProcessStatusList1, true)
		httpserver.RegisterHttpHandlerFunc(http.MethodGet, "/api/1/process/+/status/+", HandleGetProcessStatus1, true)
		httpserver.RegisterHttpHandlerFunc(http.MethodGet, "/api/1/status/+", HandleGetStatusList1, true)
		httpserver.RegisterHttpHandlerFunc(http.MethodGet, "/api/1/process/statistic", HandleGetProcessStatistic1, true)
		httpserver.RegisterHttpHandlerFunc(http.MethodPut, "/api/1/process/+/update-file/+", HandlePutUpdateFile1, true)

		g = new(guard)
		g.run()

		um = new(updateManager)
		um.updates = make(map[int]string)
	}
}

func Quit() {
	bus.Unsubscribe(base.KeyProcess, ph)
	bus.Unsubscribe(base.KeyProcessStatus, ph)
	m.quit()
}

type getProcessList1Response struct {
	httpserver.ResponseBody
	Data []*process.Process `json:"data"`
}

func HandleGetProcessList1(w http.ResponseWriter, r *http.Request) {
	var response getProcessList1Response
	var err error
	response.Data, err = process.GetProcessList()
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Result = true
	httpserver.Response(w, response)
}

type getProcess1Response struct {
	httpserver.ResponseBody
	Data *process.Process `json:"data"`
}

func HandleGetProcess1(w http.ResponseWriter, r *http.Request) {
	temp := httpserver.GetId(r.URL.Path, "process")
	if temp == "" {
		httpserver.ResponseError(w, "进程ID不存在", http.StatusBadRequest)
		return
	}
	processId, err := strconv.Atoi(temp)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusBadRequest)
		return
	}
	var response getProcess1Response
	response.Data, err = process.GetProcess(processId)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Result = true
	httpserver.Response(w, response)
}

type putProcessOperation1Request struct {
	Operation string `json:"operation"`
}

func HandlePutProcessOperation1(w http.ResponseWriter, r *http.Request) {
	var request putProcessOperation1Request
	if err := httpserver.GetRequestBody(r, &request); err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusBadRequest)
		return
	}
	temp := httpserver.GetId(r.URL.Path, "process")
	if temp == "" {
		httpserver.ResponseError(w, "无效进程ID", http.StatusBadRequest)
		return
	}
	processId, err := strconv.Atoi(temp)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := m.getProcessData(processId)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusNotFound)
		return
	}
	var response httpserver.ResponseBody
	logutils.Trace("Operation is ", request.Operation)
	switch request.Operation {
	case base.OperateStart:
		if p.process.Enable {
			_ = m.start(p)
		} else {
			httpserver.ResponseError(w, "该进程已禁用", http.StatusBadRequest)
			return
		}
	case base.OperateStop:
		_ = m.stop(p)
	case base.OperateRestart:
		if p.process.Enable {
			_ = m.restart(p)
		} else {
			httpserver.ResponseError(w, "该进程已禁用", http.StatusBadRequest)
			return
		}
	case base.OperateEnable:
		if !p.process.Enable {
			err = process.SetProcessEnable(p.process.Id, true, r)
			if err != nil {
				httpserver.ResponseError(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	case base.OperateDisable:
		if p.process.Enable {
			err = process.SetProcessEnable(p.process.Id, false, r)
			if err != nil {
				httpserver.ResponseError(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	default:
		httpserver.ResponseError(w, "无效操作", http.StatusBadRequest)
		return
	}
	response.Result = true
	httpserver.Response(w, response)
}

type getProcessStatusList1Response struct {
	httpserver.ResponseBody
	Data []*process.Status `json:"data"`
}

func HandleGetProcessStatusList1(w http.ResponseWriter, r *http.Request) {
	temp := httpserver.GetId(r.URL.Path, "process")
	if temp == "" {
		httpserver.ResponseError(w, "无效进程ID", http.StatusBadRequest)
		return
	}
	processId, err := strconv.Atoi(temp)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusBadRequest)
		return
	}
	var response getProcessStatusList1Response
	response.Data, err = process.GetStatusByProcessId(processId)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Result = true
	httpserver.Response(w, response)
}

type getProcessStatus1Response struct {
	httpserver.ResponseBody
	Data *process.Status `json:"data"`
}

func HandleGetProcessStatus1(w http.ResponseWriter, r *http.Request) {
	temp := httpserver.GetId(r.URL.Path, "process")
	if temp == "" {
		httpserver.ResponseError(w, "无效进程ID", http.StatusBadRequest)
		return
	}
	processId, err := strconv.Atoi(temp)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusBadRequest)
		return
	}
	statusType := httpserver.GetId(r.URL.Path, "status")
	if statusType == "" {
		httpserver.ResponseError(w, "无效状态类型", http.StatusBadRequest)
		return
	}
	var response getProcessStatus1Response
	response.Data, err = process.GetStatus(processId, statusType)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Result = true
	httpserver.Response(w, response)
}

type getStatusList1Response struct {
	httpserver.ResponseBody
	Data []*process.Status `json:"data"`
}

func HandleGetStatusList1(w http.ResponseWriter, r *http.Request) {
	statusType := httpserver.GetId(r.URL.Path, "status")
	if statusType == "" {
		httpserver.ResponseError(w, "无效状态类型", http.StatusBadRequest)
		return
	}
	var response getStatusList1Response
	var err error
	response.Data, err = process.GetStatusByType(statusType)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Result = true
	httpserver.Response(w, response)
}

type getProcessStatistic1Response struct {
	httpserver.ResponseBody
	Data base.Statistic `json:"data"`
}

func HandleGetProcessStatistic1(w http.ResponseWriter, r *http.Request) {
	var response getProcessStatistic1Response
	m.mutex.Lock()
	response.Data = m.statistic
	m.mutex.Unlock()
	response.Result = true
	httpserver.Response(w, response)
}

func (m *updateManager) isUpdating(processId int) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	_, ok := m.updates[processId]
	return ok
}

func (m *updateManager) insertUpdate(processId int, updateId string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.updates[processId] = updateId
}

func (m *updateManager) eraseUpdate(processId int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.updates, processId)
}

func HandlePutUpdateFile1(w http.ResponseWriter, r *http.Request) {
	defer func() {
		_ = r.Body.Close()
	}()

	temp := httpserver.GetId(r.URL.Path, "process")
	if temp == "" {
		httpserver.ResponseError(w, "无效进程ID", http.StatusBadRequest)
		return
	}
	processId, err := strconv.Atoi(temp)
	if err != nil {
		httpserver.ResponseError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if um.isUpdating(processId) {
		httpserver.ResponseError(w, "进程正在升级", http.StatusConflict)
		return
	}

	updateId := httpserver.GetId(r.URL.Path, "update-file")
	if updateId == "" {
		httpserver.ResponseError(w, "无效升级文件ID", http.StatusBadRequest)
		return
	}

	buffer, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logutils.Error("Failed to ReadAll. error: ", err)
		httpserver.ResponseError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	reader := strings.NewReader(string(buffer))
	zipReader, err := zip.NewReader(reader, int64(len(buffer)))
	if err != nil {
		logutils.Error("Failed to NewReader. error: ", err)
		httpserver.ResponseError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	um.insertUpdate(processId, updateId)
	go func() {
		notification := base.UpdateNotification {
			Status:  base.UpdateUpdating,
			Current: 0,
			Total:   len(zipReader.File),
		}
		_ = bus.PublishResource(base.KeyUpdate, base.StatusUpdated, updateId, &notification, nil)

		defer func() {
			_ = bus.PublishResource(base.KeyUpdate, base.StatusUpdated, updateId, &notification, nil)
			um.eraseUpdate(processId)
		}()

		p, err := m.getProcessData(processId)
		if err != nil {
			logutils.Error("Failed to getProcessData. error: ", err)
			notification.Status = base.UpdateFail
			return
		}
		isStart := p.isStart
		if isStart {
			if err := m.stop(p); err != nil {
				logutils.Error("Failed to stop. error: ", err)
				notification.Status = base.UpdateFail
				return
			}
		}

		destDir := p.process.Dir
		for _, file := range zipReader.File {
			logutils.Trace("File name is ", file.Name)
			path := filepath.Join(destDir, file.Name)
			if file.FileInfo().IsDir() {
				if err := os.MkdirAll(path, os.ModePerm); err != nil {
					logutils.Error("Failed to MkdirAll. error: ", err)
					notification.Status = base.UpdateFail
					return
				}
				notification.Current++
				_ = bus.PublishResource(base.KeyUpdate, base.StatusUpdated, updateId, &notification, nil)
			} else {
				if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
					logutils.Error("Failed to MkdirAll. error: ", err)
					notification.Status = base.UpdateFail
					return
				}

				inFile, err := file.Open()
				if err != nil {
					logutils.Error("Failed to Open. error: ", err)
					notification.Status = base.UpdateFail
					return
				}

				outFile, err := os.OpenFile(path, os.O_WRONLY | os.O_CREATE | os.O_TRUNC, file.Mode())
				if err != nil {
					logutils.Error("Failed to OpenFile. error: ", err)
					notification.Status = base.UpdateFail
					_ = inFile.Close()
					return
				}

				_, err = io.Copy(outFile, inFile)
				_ = inFile.Close()
				_ = outFile.Close()

				if err != nil {
					logutils.Error("Failed to Copy. error: ", err)
					notification.Status = base.UpdateFail
					return
				}

				notification.Current++
				_ = bus.PublishResource(base.KeyUpdate, base.StatusUpdated, updateId, &notification, nil)
			}
		}

		if isStart {
			if err := m.start(p); err != nil {
				logutils.Error("Failed to start. error: ", err)
			}
		}
		notification.Status = base.UpdateSuccess
	}()

	var response httpserver.ResponseBody
	response.Result = true
	httpserver.Response(w, response)
}
