package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const rtspPort = 554

func logRequest(handlerFunc http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		log.Printf("Received request: Method=%s URL=%s From=%s", r.Method, r.URL.Path, r.RemoteAddr)

		recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		handlerFunc(recorder, r)

		log.Printf("Responded: Status=%d Duration=%s", recorder.statusCode, time.Since(start))
	}
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func checkRTSP(ip string) bool {
	address := fmt.Sprintf("%s:%d", ip, rtspPort)
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func getIPsInNetwork(network *net.IPNet) []string {
	var ips []string
	for ip := network.IP.Mask(network.Mask); network.Contains(ip); incrementIP(ip) {
		ips = append(ips, ip.String())
	}
	return ips
}

func incrementIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func scanIPs(ips []string) []string {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var devices []string

	for _, ip := range ips {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			if checkRTSP(ip) {
				mu.Lock()
				devices = append(devices, ip)
				mu.Unlock()
			}
		}(ip)
	}

	wg.Wait()
	return devices
}

func getLocalNetworks() ([]*net.IPNet, error) {
	var networks []*net.IPNet

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			log.Printf("Error getting addresses for interface %s: %v", iface.Name, err)
			continue
		}

		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
				networks = append(networks, ipNet)
				log.Printf("Found network: Interface=%s IP=%s Network=%s", iface.Name, ipNet.IP, ipNet)
			}
		}
	}

	if len(networks) == 0 {
		log.Println("No active networks found.")
	}

	return networks, nil
}

func handleGetAllRTSPDevices(w http.ResponseWriter, r *http.Request) {
	networks, err := getLocalNetworks()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error determining local networks: %v", err), http.StatusInternalServerError)
		return
	}

	var allDevices []string
	for _, network := range networks {
		ips := getIPsInNetwork(network)
		devices := scanIPs(ips)
		allDevices = append(allDevices, devices...)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allDevices)
	log.Println("Found cameras:", allDevices)
}

func main() {
	fmt.Println("Starting server on :7654...")
	http.HandleFunc("/get_all_onvif_cameras/", logRequest(handleGetAllRTSPDevices))

	if err := http.ListenAndServe(":7654", nil); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}
