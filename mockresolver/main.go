package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/miekg/dns"
)

func main() {
	log.Println("Starting mock DNS resolver")

	// Setup DNS server
	dns.HandleFunc("tailscale.svc.cluster.local.", handleDNSRequest)
	dns.HandleFunc(".", handleDefault)

	// Start server on UDP
	server := &dns.Server{Addr: ":53", Net: "udp"}
	go func() {
		log.Printf("Starting UDP DNS server on :53")
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start UDP server: %s", err.Error())
		}
	}()

	// Start server on TCP
	tcpServer := &dns.Server{Addr: ":53", Net: "tcp"}
	go func() {
		log.Printf("Starting TCP DNS server on :53")
		if err := tcpServer.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start TCP server: %s", err.Error())
		}
	}()

	// Wait for SIGINT or SIGTERM
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("Signal (%v) received, stopping", s)

	// Shutdown servers
	if err := server.Shutdown(); err != nil {
		log.Printf("Error shutting down UDP server: %v", err)
	}
	if err := tcpServer.Shutdown(); err != nil {
		log.Printf("Error shutting down TCP server: %v", err)
	}
}

func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	for _, q := range r.Question {
		log.Printf("Query: %s %s", dns.TypeToString[q.Qtype], q.Name)

		if q.Name == "tsdnsreflector.tailscale.svc.cluster.local." {
			if q.Qtype == dns.TypeA {
				log.Printf("Responding with A record: 157.48.0.1")
				m.Answer = append(m.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP("157.48.0.1").To4(),
				})
			} else if q.Qtype == dns.TypeAAAA {
				log.Printf("Responding with AAAA record: fdbb:cbf8:2702::9d30")
				m.Answer = append(m.Answer, &dns.AAAA{
					Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60},
					AAAA: net.ParseIP("fdbb:cbf8:2702::9d30").To16(),
				})
			}
		}
	}

	w.WriteMsg(m)
}

func handleDefault(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = false

	w.WriteMsg(m)
}
