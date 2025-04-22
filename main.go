package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/miekg/dns"
)

var (
	siteID      int
	ipv6Domain  string
	ipv4Domain  string
	dnsResolver *net.Resolver
	serverPort  string
	force4via6  bool
)

func init() {
	var err error
	
	siteIDStr, exists := os.LookupEnv("SITE_ID")
	if !exists {
		log.Fatalf("SITE_ID environment variable is required")
	}
	
	siteID, err = strconv.Atoi(siteIDStr)
	if err != nil {
		log.Fatalf("Invalid SITE_ID: %v", err)
	}
	if siteID < 0 || siteID > 65535 {
		log.Fatalf("SITE_ID must be between 0 and 65535")
	}

	ipv6Domain, exists = os.LookupEnv("IPV6_DOMAIN")
	if !exists {
		log.Fatalf("IPV6_DOMAIN environment variable is required")
	}

	ipv4Domain, exists = os.LookupEnv("IPV4_DOMAIN")
	if !exists {
		log.Fatalf("IPV4_DOMAIN environment variable is required")
	}

	// Ensure domains end with a dot for proper DNS comparison
	if !strings.HasSuffix(ipv6Domain, ".") {
		ipv6Domain = ipv6Domain + "."
	}
	if !strings.HasSuffix(ipv4Domain, ".") {
		ipv4Domain = ipv4Domain + "."
	}
	
	// Check for FORCE_4VIA6 flag
	force4via6Str, exists := os.LookupEnv("FORCE_4VIA6")
	if exists {
		if strings.ToLower(force4via6Str) == "true" || force4via6Str == "1" {
			force4via6 = true
			log.Printf("FORCE_4VIA6 enabled: A queries will be answered with AAAA records")
		} else if strings.ToLower(force4via6Str) == "false" || force4via6Str == "0" {
			force4via6 = false
			log.Printf("FORCE_4VIA6 disabled: A queries will be answered with A records")
		} else {
			log.Printf("Invalid FORCE_4VIA6 value: %s - must be 'true', 'false', '1', or '0'. Defaulting to true", force4via6Str)
			force4via6 = true
		}
	} else {
		// Default to true for backward compatibility
		force4via6 = true
		log.Printf("FORCE_4VIA6 not specified, defaulting to true (A queries will be answered with AAAA records)")
	}
	
	// Check for custom server port
	if port, exists := os.LookupEnv("PORT"); exists && port != "" {
		// Validate the port is a number
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			log.Fatalf("Invalid PORT value: %s - must be between 1 and 65535", port)
		}
		serverPort = port
		log.Printf("Using custom server port: %s", serverPort)
	} else {
		// Default to port 53 if not specified
		serverPort = "53"
		log.Printf("Using default DNS port: %s", serverPort)
	}

	// Set up the DNS resolver
	if customResolver, exists := os.LookupEnv("DNS_RESOLVER"); exists && customResolver != "" {
		// If a custom resolver was specified, use it
		// Format should be host:port (e.g., 8.8.8.8:53)
		log.Printf("Using custom DNS resolver: %s", customResolver)
		
		dnsResolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{}
				return d.DialContext(ctx, "udp", customResolver)
			},
		}
	} else {
		// Otherwise use the system resolver
		log.Printf("Using system DNS resolver")
		dnsResolver = net.DefaultResolver
	}
}

// IPv4ToTailscale4via6 converts an IPv4 address to a Tailscale 4via6 IPv6 address
func IPv4ToTailscale4via6(ipv4 net.IP, siteID int) (net.IP, error) {
	if ipv4 == nil || ipv4.To4() == nil {
		return nil, fmt.Errorf("invalid IPv4 address")
	}

	// Tailscale 4via6 format: fd7a:115c:a1e0:b1a:0:XXXX:YYYY:YYYY
	// Where XXXX is the site ID and YYYY:YYYY is the IPv4 address in hex
	ipv4Bytes := ipv4.To4()
	ipv6 := make(net.IP, 16)

	// Set the fixed prefix fd7a:115c:a1e0:b1a
	ipv6[0] = 0xfd
	ipv6[1] = 0x7a
	ipv6[2] = 0x11
	ipv6[3] = 0x5c
	ipv6[4] = 0xa1
	ipv6[5] = 0xe0
	ipv6[6] = 0x0b
	ipv6[7] = 0x1a

	// Set the site ID (0:XXXX)
	ipv6[8] = 0x00
	ipv6[9] = 0x00
	ipv6[10] = byte(siteID >> 8)
	ipv6[11] = byte(siteID)

	// Set the IPv4 address (YYYY:YYYY)
	ipv6[12] = ipv4Bytes[0]
	ipv6[13] = ipv4Bytes[1]
	ipv6[14] = ipv4Bytes[2]
	ipv6[15] = ipv4Bytes[3]

	return ipv6, nil
}

func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	ctx := context.Background()

	for _, q := range r.Question {
		log.Printf("Received query for %s (type: %d)", q.Name, q.Qtype)

		// Check if the query is for our IPv6 domain
		if strings.HasSuffix(q.Name, ipv6Domain) {
			// For AAAA queries, first try to resolve directly as IPv6
			if q.Qtype == dns.TypeAAAA {
				log.Printf("Attempting direct AAAA lookup for %s", q.Name)
				// Try to get native AAAA records first
				ips, err := dnsResolver.LookupIP(ctx, "ip6", strings.TrimSuffix(q.Name, "."))
				
				// If we found actual IPv6 addresses, return them directly
				if err == nil && len(ips) > 0 {
					hasValidIPv6 := false
					for _, ip := range ips {
						if ip.To4() == nil { // This is a proper IPv6 address
							log.Printf("Found native IPv6 address: %s", ip)
							aaaa := &dns.AAAA{
								Hdr: dns.RR_Header{
									Name:   q.Name,
									Rrtype: dns.TypeAAAA,
									Class:  dns.ClassINET,
									Ttl:    300,
								},
								AAAA: ip,
							}
							m.Answer = append(m.Answer, aaaa)
							hasValidIPv6 = true
						}
					}
					
					// If we already have valid IPv6 addresses, continue to the next question
					if hasValidIPv6 {
						log.Printf("Returning native IPv6 addresses for %s", q.Name)
						continue
					}
				}
				
				// If no IPv6 addresses found, log and continue to IPv4 conversion
				log.Printf("No native IPv6 addresses found, falling back to IPv4 conversion")
			} else if q.Qtype == dns.TypeA && !force4via6 {
				// If this is an A query and FORCE_4VIA6 is false, handle as normal A query
				log.Printf("A record query with FORCE_4VIA6=false, returning normal A record")
				
				// Convert from IPv6 domain to IPv4 domain for lookup
				ipv4Name := strings.TrimSuffix(q.Name, ipv6Domain) + ipv4Domain
				log.Printf("Looking up A record for %s", ipv4Name)

				// Look up the A record
				ips, err := dnsResolver.LookupIP(ctx, "ip4", strings.TrimSuffix(ipv4Name, "."))
				if err != nil {
					log.Printf("Error looking up A record: %v", err)
					continue
				}

				// Return A records directly
				for _, ip := range ips {
					if ip.To4() != nil {
						log.Printf("Returning A record: %s", ip)
						a := &dns.A{
							Hdr: dns.RR_Header{
								Name:   q.Name,
								Rrtype: dns.TypeA,
								Class:  dns.ClassINET,
								Ttl:    300,
							},
							A: ip.To4(),
						}
						m.Answer = append(m.Answer, a)
					}
				}
				
				// Continue to next question since we've handled this one
				continue
			}
			
			// If we get here, we need to do the IPv4-to-IPv6 conversion
			// This happens for:
			// 1. AAAA queries that didn't find native IPv6 addresses
			// 2. A queries when FORCE_4VIA6 is true
			
			// Convert from IPv6 domain to IPv4 domain
			ipv4Name := strings.TrimSuffix(q.Name, ipv6Domain) + ipv4Domain
			log.Printf("Looking up A record for %s", ipv4Name)

			// Look up the A record
			ips, err := dnsResolver.LookupIP(ctx, "ip4", strings.TrimSuffix(ipv4Name, "."))
			if err != nil {
				log.Printf("Error looking up A record: %v", err)
				continue
			}

			// Process any IPv4 addresses we found
			for _, ip := range ips {
				if ip.To4() != nil {
					// Convert IPv4 to Tailscale 4via6 format
					ipv6, err := IPv4ToTailscale4via6(ip, siteID)
					if err != nil {
						log.Printf("Error converting to IPv6: %v", err)
						continue
					}

					log.Printf("Converted %s to %s", ip, ipv6)
					
					// For AAAA queries, return the IPv6 address
					if q.Qtype == dns.TypeAAAA {
						aaaa := &dns.AAAA{
							Hdr: dns.RR_Header{
								Name:   q.Name,
								Rrtype: dns.TypeAAAA,
								Class:  dns.ClassINET,
								Ttl:    300,
							},
							AAAA: ipv6,
						}
						m.Answer = append(m.Answer, aaaa)
					} else if q.Qtype == dns.TypeA && force4via6 {
						// For A queries to the IPv6 domain when FORCE_4VIA6 is true,
						// return the IPv6 address as AAAA record
						log.Printf("FORCE_4VIA6=true: Returning AAAA record for A query")
						aaaa := &dns.AAAA{
							Hdr: dns.RR_Header{
								Name:   q.Name,
								Rrtype: dns.TypeAAAA,
								Class:  dns.ClassINET,
								Ttl:    300,
							},
							AAAA: ipv6,
						}
						m.Answer = append(m.Answer, aaaa)
					}
				}
			}
		} else {
			// Forward other queries to the system resolver
			log.Printf("Forwarding query for %s (type %d)", q.Name, q.Qtype)

			switch q.Qtype {
			case dns.TypeA:
				ips, err := dnsResolver.LookupIP(ctx, "ip4", strings.TrimSuffix(q.Name, "."))
				if err == nil {
					for _, ip := range ips {
						if ip.To4() != nil {
							a := &dns.A{
								Hdr: dns.RR_Header{
									Name:   q.Name,
									Rrtype: dns.TypeA,
									Class:  dns.ClassINET,
									Ttl:    300,
								},
								A: ip.To4(),
							}
							m.Answer = append(m.Answer, a)
						}
					}
				}
			case dns.TypeAAAA:
				ips, err := dnsResolver.LookupIP(ctx, "ip6", strings.TrimSuffix(q.Name, "."))
				if err == nil {
					for _, ip := range ips {
						if ip.To4() == nil {
							aaaa := &dns.AAAA{
								Hdr: dns.RR_Header{
									Name:   q.Name,
									Rrtype: dns.TypeAAAA,
									Class:  dns.ClassINET,
									Ttl:    300,
								},
								AAAA: ip,
							}
							m.Answer = append(m.Answer, aaaa)
						}
					}
				}
			}
		}
	}

	w.WriteMsg(m)
}

func main() {
	log.Printf("Starting tsdnsreflector with SITE_ID=%d, IPV6_DOMAIN=%s, IPV4_DOMAIN=%s", 
		siteID, ipv6Domain, ipv4Domain)

	// Create a new DNS server
	dns.HandleFunc(".", handleDNSRequest)

	// Start the DNS server
	go func() {
		server := &dns.Server{Addr: ":" + serverPort, Net: "udp"}
		log.Printf("Starting DNS server on %s/udp", server.Addr)
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start UDP server: %v", err)
		}
	}()

	go func() {
		server := &dns.Server{Addr: ":" + serverPort, Net: "tcp"}
		log.Printf("Starting DNS server on %s/tcp", server.Addr)
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start TCP server: %v", err)
		}
	}()

	// Keep the server running
	select {}
} 