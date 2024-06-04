package main

import (
	"fmt"
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
	ID     string
	Group  *Group
	Cache  map[string]DNSResponse
	Mutex  sync.Mutex
	Server string // DNS resolver address
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
	fmt.Println("Adding new client")
	return &Client{
		ID:     id,
		Cache:  make(map[string]DNSResponse),
		Server: server,
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
	fmt.Println("QueryDNS started, looking cache", domain)
	// First check the client's cache
	c.Mutex.Lock()
	response, found := c.Cache[domain]
	c.Mutex.Unlock()
	fmt.Println("Cache response", response)
	if found && time.Since(response.Timestamp) < time.Hour {
		fmt.Println("Cache found", response.IPAddress)
		return response.IPAddress, nil
	}
	fmt.Println("Cache not found")
	// If not found, relay the query to another client in the group
	for _, peer := range c.Group.Clients {
		fmt.Println("relay the query to another client in group", peer)
		if peer != c {
			peer.Mutex.Lock()
			response, found = peer.Cache[domain]
			peer.Mutex.Unlock()
			fmt.Println("QueryDNS: Checking relay client's response")
			if found && time.Since(response.Timestamp) < time.Hour {
				fmt.Println("QueryDNS: Found cache relay client's response", found)
				c.Mutex.Lock()
				c.Cache[domain] = response
				c.Mutex.Unlock()
				return response.IPAddress, nil
			}
		}
	}
	fmt.Println("QueryDNS: Cache not existed relay client's response")
	// If still not found, query the DNS resolver
	ip, err := c.queryDNSResolver(domain)
	fmt.Println("QueryDNS: Return queryDNSResolver response", ip, err)
	if err != nil {
		return "", err
	}

	// Cache the result
	c.Mutex.Lock()
	c.Cache[domain] = DNSResponse{IPAddress: ip, Timestamp: time.Now()}
	c.Mutex.Unlock()

	return ip, nil
}

func (c *Client) queryDNSResolver(domain string) (string, error) {
	fmt.Println("queryDNSResolver: Sending query to DNS resolver")

	client := new(dns.Client)
	message := new(dns.Msg)
	message.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	message.RecursionDesired = true

	r, _, err := client.Exchange(message, c.Server)
	if err != nil {
		fmt.Println("queryDNSResolver: DNS query error", err)
		return "", err
	}

	if r.Rcode != dns.RcodeSuccess {
		fmt.Println("queryDNSResolver: DNS query failed with Rcode", r.Rcode)
		return "", fmt.Errorf("DNS query failed with Rcode %d", r.Rcode)
	}

	for _, answer := range r.Answer {
		if a, ok := answer.(*dns.A); ok {
			return a.A.String(), nil
		}
	}

	return "", fmt.Errorf("No A record found for domain %s", domain)
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

	// Assuming we want to test with the first client
	clientA := groupManager.Groups[0].Clients[0]
	fmt.Println("Client A triggers query")

	// Client A sends a query
	ip, err := clientA.QueryDNS("deeptrade.co")
	fmt.Println("Client query response", ip)
	if err != nil {
		fmt.Println("Error querying DNS:", err)
	} else {
		fmt.Println("IP address for google.com:", ip)
	}
}
