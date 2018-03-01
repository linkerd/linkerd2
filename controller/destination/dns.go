package destination

import (
	"fmt"
	"net"
	"sync"
	"time"

	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/util"
	log "github.com/sirupsen/logrus"
)

type DnsListener interface {
	Update(add []common.TcpAddress, remove []common.TcpAddress)
}

type DnsWatcher struct {
	hosts map[string]*informer
	mutex sync.Mutex
}

func NewDnsWatcher() *DnsWatcher {
	return &DnsWatcher{
		hosts: make(map[string]*informer),
	}
}

func (w *DnsWatcher) Subscribe(host string, listener DnsListener) error {
	log.Printf("Establishing dns watch on host %s", host)

	w.mutex.Lock()
	defer w.mutex.Unlock()

	informer, ok := w.hosts[host]
	if !ok {
		informer = newInformer(host)
		go informer.run()
		w.hosts[host] = informer
	}

	informer.add(listener)

	return nil
}

func (w *DnsWatcher) Unsubscribe(host string, listener DnsListener) error {
	log.Printf("Stopping dns watch on host %s", host)

	w.mutex.Lock()
	defer w.mutex.Unlock()

	informer, ok := w.hosts[host]
	if !ok {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", host)
	}

	informer.mutex.Lock()
	defer informer.mutex.Unlock()

	for i, v := range informer.listeners {
		if v == listener {
			num := len(informer.listeners)
			if num == 1 {
				// last subscription being removed, close me up!
				informer.stopCh <- struct{}{}
				delete(w.hosts, host)
				return nil
			} else if num == i+1 {
				informer.listeners = informer.listeners[:i]
			} else {
				informer.listeners = append(informer.listeners[:i], informer.listeners[i+1:]...)
			}
		}
	}

	return nil
}

type informer struct {
	host      string
	addresses []common.TcpAddress
	listeners []DnsListener
	mutex     sync.Mutex
	stopCh    chan struct{}
}

func newInformer(host string) *informer {
	i := &informer{
		host:      host,
		addresses: make([]common.TcpAddress, 0),
		listeners: make([]DnsListener, 0),
		stopCh:    make(chan struct{}),
	}

	return i
}

func (i *informer) run() {
	ticker := time.NewTicker(10 * time.Second)
	for {
		addrs, err := net.LookupHost(i.host)
		if err != nil {
			log.Printf("host lookup failed [%s]: %s", i.host, err)
		} else {
			addresses := make([]common.TcpAddress, 0)
			for _, addr := range addrs {
				ip, err := util.ParseIPV4(addr)
				if err != nil {
					log.Printf("%s is not a valid IP address", addr)
				} else {
					address := common.TcpAddress{Ip: ip, Port: 80}
					addresses = append(addresses, address)
				}
			}
			i.update(addresses)
		}

		select {
		case <-ticker.C:
			continue
		case <-i.stopCh:
			ticker.Stop()
			return
		}
	}
}

func (i *informer) update(newAddresses []common.TcpAddress) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	add, remove := util.DiffAddresses(i.addresses, newAddresses)
	for _, listener := range i.listeners {
		listener.Update(add, remove)
	}
	i.addresses = newAddresses
}

func (i *informer) add(listener DnsListener) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	listener.Update(i.addresses, nil)
	i.listeners = append(i.listeners, listener)
}
