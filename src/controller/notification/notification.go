package notification

import (
	"encoding/json"
	"github.com/infinit-lab/puppy/src/model/base"
	"github.com/infinit-lab/yolanda/bus"
	"github.com/infinit-lab/yolanda/httpserver"
	"github.com/infinit-lab/yolanda/logutils"
	"net/http"
	"net/url"
	"sync"
)

func init() {
	logutils.Trace("Initializing controller notification...")
	n := new(notification)
	n.nodes = make(map[int]*node)
	bus.Subscribe(base.KeyAll, n)
	httpserver.RegisterWebsocketHandler("/ws/1/notification", n, true)
}

type node struct {
	token string
	r     *http.Request
}

type notification struct {
	mutex sync.Mutex
	nodes map[int]*node
}

func (n *notification) Handle(key int, value *bus.Resource) {
	switch key {
	case base.KeyToken:
		switch value.Status {
		case base.StatusCreated, base.StatusUpdated:
			return
		}
	case base.KeyPassword:
		return
	}
	n.notify(key, value)
}

func (n *notification) NewConnection(nodeId int, r *http.Request) {
	form, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		_ = httpserver.WebsocketClose(nodeId)
		return
	}
	tokens, ok := form["token"]
	if !ok || len(tokens) == 0 {
		_ = httpserver.WebsocketClose(nodeId)
		return
	}
	n.mutex.Lock()
	defer n.mutex.Unlock()

	for _, v := range n.nodes {
		if v.token == tokens[0] {
			_ = httpserver.WebsocketClose(nodeId)
			return
		}
	}

	tempNode := new(node)
	tempNode.token = tokens[0]
	tempNode.r = r
	n.nodes[nodeId] = tempNode
}

func (n *notification) Disconnected(nodeId int) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	delete(n.nodes, nodeId)
}

func (n *notification) ReadBytes(nodeId int, bytes []byte) {

}

func (n *notification) ReadMessage(nodeId int, message []byte) {

}

type notificationBody struct {
	Key    int    `json:"key"`
	Status int    `json:"status"`
	Id     string `json:"id"`
}

func (n *notification) notify(key int, resource *bus.Resource) {
	body := notificationBody{
		Key:    key,
		Status: resource.Status,
		Id:     resource.Id,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return
	}

	var nodes []int
	n.mutex.Lock()
	for k, _ := range n.nodes {
		nodes = append(nodes, k)
	}
	n.mutex.Unlock()

	for _, nodeId := range nodes {
		_ = httpserver.WebsocketWriteMessage(nodeId, data)
	}
}
