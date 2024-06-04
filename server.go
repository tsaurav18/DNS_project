package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/miekg/dns"
)

const (
	GroupSize = 15
)

type DNSResponse struct {
	IPAddress string
	Timestamp time.Time
}

type Client struct {
	ID        string
	Group     *Group
	Cache     map[string]DNSResponse
	Mutex     sync.Mutex
	Server    string // DNS resolver address
	CacheFile string
}

type Group struct {
	ID      string
	Clients []*Client
	Mutex   sync.Mutex
}

type GroupManager struct {
	Groups []*Group
	Mutex  sync.Mutex
}

type Config struct {
	Clients []struct {
		ID     string `toml:"id"`
		Server string `toml:"server"`
	} `toml:"clients"`
}

func NewClient(id string, server string) *Client {
	cacheFile := fmt.Sprintf("%s_cache.json", id)
	client := &Client{
		ID:        id,
		Cache:     make(map[string]DNSResponse),
		Server:    server,
		CacheFile: cacheFile,
	}
	client.loadCache()
	return client
}

func (c *Client) loadCache() {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	if _, err := os.Stat(c.CacheFile); err == nil {
		data, err := ioutil.ReadFile(c.CacheFile)
		if err == nil {
			json.Unmarshal(data, &c.Cache)
		}
	}
}

func (c *Client) saveCache() {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	data, err := json.Marshal(c.Cache)
	if err == nil {
		ioutil.WriteFile(c.CacheFile, data, 0644)
	}
}

func (gm *GroupManager) AddClientToGroup(client *Client) {
	gm.Mutex.Lock()
	defer gm.Mutex.Unlock()

	// Find a group with space or create a new one
	for _, group := range gm.Groups {
		if len(group.Clients) < GroupSize {
			group.Clients = append(group.Clients, client)
			client.Group = group
			return
		}
	}

	// Create a new group
	newGroup := &Group{
		ID:      fmt.Sprintf("Group-%d", len(gm.Groups)+1),
		Clients: []*Client{client},
	}
	gm.Groups = append(gm.Groups, newGroup)
	client.Group = newGroup
}

func (c *Client) QueryDNS(domain string) (string, error) {
	c.Mutex.Lock()
	response, found := c.Cache[domain]
	c.Mutex.Unlock()

	if found && time.Since(response.Timestamp) < time.Hour {
		fmt.Println("Domain name found in cache", c)
		return response.IPAddress, nil
	}

	for _, peer := range c.Group.Clients {
		if peer != c {
			peer.Mutex.Lock()
			fmt.Println("Checking domain name in next client's cache...")
			response, found = peer.Cache[domain]
			peer.Mutex.Unlock()
			if found && time.Since(response.Timestamp) < time.Hour {
				fmt.Println("Domain found in client's cache...", found)
				c.Mutex.Lock()
				c.Cache[domain] = response
				c.Mutex.Unlock()
				c.saveCache()
				return response.IPAddress, nil
			}
		}
	}
	fmt.Println("Domain not found in client's cache, calling QueryDNSResolver", domain, "\n")
	ip, err := c.queryDNSResolver(domain)
	fmt.Println("\nIP address is found", ip)
	if err != nil {
		return "", err
	}

	c.Mutex.Lock()
	c.Cache[domain] = DNSResponse{IPAddress: ip, Timestamp: time.Now()}
	c.Mutex.Unlock()
	c.saveCache()

	return ip, nil
}

func (c *Client) queryDNSResolver(domain string) (string, error) {
	fmt.Printf("queryDNSResolver: query: %s\n", domain)
	// Perform the DNS query using net.LookupHost
	ips, err := net.LookupHost(domain)
	if err != nil {
		return "", fmt.Errorf("failed to resolve domain %s: %v", domain, err)
	}

	// Return the first IP address found
	if len(ips) > 0 {
		ip := ips[0]
		fmt.Printf("queryDNSResolver: resolver response: %s\n", ip)
		return ip, nil
	}

	return "", fmt.Errorf("no A record found for domain %s", domain)
}

func main() {
	fmt.Println("Starting....")
	// Load the configuration
	var config Config
	if _, err := toml.DecodeFile("config.toml", &config); err != nil {
		fmt.Println("Error loading config:", err)
		return
	}

	// Create a group manager
	groupManager := &GroupManager{}

	// Create clients and add them to groups based on the configuration
	for _, clientConfig := range config.Clients {
		client := NewClient(clientConfig.ID, clientConfig.Server)
		groupManager.AddClientToGroup(client)
	}
	fmt.Println("All clients added successfully....")

	dns.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		for _, q := range r.Question {
			domain := q.Name
			fmt.Println("Looking for client domain: ", domain)
			ip, err := groupManager.Groups[0].Clients[0].QueryDNS(domain)
			m := new(dns.Msg)
			m.SetReply(r)
			if err != nil {
				m.Rcode = dns.RcodeServerFailure
			} else {
				rr, _ := dns.NewRR(fmt.Sprintf("%s A %s", domain, ip))
				m.Answer = append(m.Answer, rr)
			}
			w.WriteMsg(m)
		}
	})

	server := &dns.Server{Addr: ":8053", Net: "udp"}
	fmt.Printf("Starting server on :8053\n")
	err := server.ListenAndServe()
	if err != nil {
		fmt.Printf("Failed to start server: %s\n", err.Error())
	}
}
