package main

import (
	"bufio"
	"fmt"
	"github.com/miekg/dns"
	"os"
	"strings"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("Enter domain name: ")
		domain, _ := reader.ReadString('\n')
		domain = strings.TrimSpace(domain)

		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(domain), dns.TypeA)

		c := new(dns.Client)
		in, _, err := c.Exchange(m, "127.0.0.1:8053")
		if err != nil {
			fmt.Printf("Failed to get DNS response: %v\n", err)
			continue
		}

		if len(in.Answer) > 0 {
			if a, ok := in.Answer[0].(*dns.A); ok {
				fmt.Printf("IP address for %s: %s\n", domain, a.A.String())
				continue
			}
		}

		fmt.Printf("No IP address found for %s\n", domain)
	}
}
