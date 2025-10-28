// GOOS=darwin GOARCH=arm64 go build -o client-macos .
// 실행하면 client-macos 파일 생김

// 윈도우용 빌드 go build -o client-windows.exe .
// 윈도우 실행은 client-windows.exe

// 리눅스 go build -o client-linux .

package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// 클라이언트 주소 정보 구조체 (서버와 동일)
type ClientInfo struct {
	PublicIP  string `json:"public_ip"`
	PrivateIP string `json:"private_ip"`
	Port      string `json:"port"`
}

// --- 전역 변수: 여러 피어를 관리하기 위한 구조 ---
var (
	udpConn  *net.UDPConn
	myInfo   ClientInfo
	peers    = make(map[string]*net.UDPAddr) // 연결된 피어 목록 관리 (key: "ip:port" 문자열)
	peersMux = &sync.Mutex{}                 // 여러 고루틴이 peers 맵에 동시 접근하는 것을 막음
)

// --- 헬퍼 함수: IP 주소 가져오기 ---
func getPrivateIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func getPublicIP() string {
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(body))
}

// --- 메인 함수: 프로그램 시작점 ---
func main() {
	// 1. 프로그램 실행 시 방 이름을 입력받음
	if len(os.Args) < 2 {
		log.Fatalf("사용법: go run client.go [방_이름]")
	}
	roomName := os.Args[1]

	// 2. UDP 리스너 시작 및 내 정보 설정
	var err error
	udpConn, err = net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		log.Fatalf("UDP 리스너 시작 실패: %v", err)
	}
	defer udpConn.Close()

	myInfo.Port = strconv.Itoa(udpConn.LocalAddr().(*net.UDPAddr).Port)
	myInfo.PrivateIP = getPrivateIP()
	myInfo.PublicIP = getPublicIP()
	log.Printf("내 정보 - 공인IP: %s, 사설IP: %s, 포트: %s", myInfo.PublicIP, myInfo.PrivateIP, myInfo.Port)

	// 3. 방 이름이 포함된 URL로 시그널링 서버에 접속
	serverAddr := fmt.Sprintf("ws://solana1000.synology.me:8080/ws?room=%s", roomName)
	log.Printf("'%s' 방에 접속 시도...", roomName)

	ws, _, err := websocket.DefaultDialer.Dial(serverAddr, nil)
	if err != nil {
		log.Fatalf("WebSocket 접속 실패: %v", err)
	}
	defer ws.Close()
	log.Println("시그널링 서버에 접속 성공")

	// 4. 서버에 내 정보 전송
	if err := ws.WriteJSON(myInfo); err != nil {
		log.Fatalf("서버에 정보 전송 실패: %v", err)
	}

	// 5. 백그라운드에서 서버 및 다른 피어의 메시지를 계속 듣기 시작
	go listenWebSocket(ws)
	go listenUDP()

	// 6. 메인 스레드에서는 사용자 입력을 받아 메시지 전송
	handleUserInput()
}

// --- 백그라운드 동작 함수들 ---

// 서버로부터 피어 목록을 실시간으로 받아서 처리
func listenWebSocket(ws *websocket.Conn) {
	for {
		var receivedPeers []ClientInfo
		if err := ws.ReadJSON(&receivedPeers); err != nil {
			log.Println("WebSocket 연결이 끊어졌습니다:", err)
			return
		}

		peersMux.Lock() // peers 맵 수정을 위해 잠금
		activePeers := make(map[string]bool)

		// 서버가 보내준 최신 목록을 기준으로 연결 시도
		for _, peerInfo := range receivedPeers {
			var targetIP string
			// 나와 상대방의 공인 IP가 같으면, 같은 네트워크에 있다는 의미
			if myInfo.PublicIP != "" && myInfo.PublicIP == peerInfo.PublicIP {
				targetIP = peerInfo.PrivateIP // 사설 IP로 연결 시도
			} else {
				targetIP = peerInfo.PublicIP // 다른 네트워크면 공인 IP로 연결 시도
			}
			peerAddrStr := targetIP + ":" + peerInfo.Port
			activePeers[peerAddrStr] = true

			// 아직 연결 목록에 없는 새로운 피어라면 연결 시도 (홀 펀칭)
			if _, exists := peers[peerAddrStr]; !exists {
				go connectToPeer(peerAddrStr)
			}
		}

		// 최신 목록에 없는 피어는 연결 목록에서 제거 (나간 사용자 처리)
		for addrStr := range peers {
			if !activePeers[addrStr] {
				log.Printf("피어 연결 해제: %s", addrStr)
				delete(peers, addrStr)
			}
		}
		peersMux.Unlock() // peers 맵 수정 완료 후 잠금 해제
	}
}

// 특정 피어에게 UDP 홀 펀칭 시도
func connectToPeer(addrStr string) {
	peerAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		log.Printf("주소 (%s) 해석 실패: %v", addrStr, err)
		return
	}
	log.Printf("새로운 피어(%s)에게 홀 펀칭 시도...", addrStr)
	for i := 0; i < 10; i++ {
		udpConn.WriteToUDP([]byte("펀칭!"), peerAddr)
		time.Sleep(100 * time.Millisecond)
	}
}

// 모든 피어로부터 오는 UDP 메시지를 처리
func listenUDP() {
	buffer := make([]byte, 1500)
	for {
		n, addr, err := udpConn.ReadFromUDP(buffer)
		if err != nil {
			log.Println("UDP 리스닝 종료:", err)
			return
		}
		addrStr := addr.String()

		peersMux.Lock()
		// 처음 응답을 받은 피어라면, 연결 성공으로 간주하고 목록에 추가
		if _, exists := peers[addrStr]; !exists {
			log.Printf("새로운 피어(%s)로부터 첫 응답! 연결 목록에 추가.", addrStr)
			peers[addrStr] = addr
		}
		peersMux.Unlock()

		log.Printf("UDP 메시지 수신 from %s: %s", addr.String(), string(buffer[:n]))
	}
}

// 사용자 입력을 받아 연결된 모든 피어에게 메시지 전송
func handleUserInput() {
	log.Println("채팅 시작 (메시지 입력 후 Enter)")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()

		peersMux.Lock()
		if len(peers) == 0 {
			log.Println("아직 연결된 피어가 없습니다.")
		} else {
			log.Printf("%d명의 피어에게 메시지 전송: %s", len(peers), text)
			// 연결된 모든 피어에게 메시지 브로드캐스트
			for _, peerAddr := range peers {
				udpConn.WriteToUDP([]byte(text), peerAddr)
			}
		}
		peersMux.Unlock()
	}
}
