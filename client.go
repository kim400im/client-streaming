// GOOS=darwin GOARCH=arm64 go build -o client-macos .
// 실행하면 client-macos 파일 생김

// 윈도우용 빌드 go build -o client-windows.exe .
// 윈도우 실행은 client-windows.exe

package main

import (
	"bufio" // JSON 처리를 위해 남겨둡니다.
	"encoding/json"
	"log"
	"net"
	"os"
	"strconv" // 숫자 <-> 문자열 변환을 위해 필수
	"time"    // 지속적인 펀칭을 위해 필수

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

// 서버와 통신을 위해 Message 구조체를 남겨둡니다.
type Message struct {
	Body string `json:"body"`
}

var otherPeerAddr *net.UDPAddr

var udpConn *net.UDPConn // udpConn을 전역 변수로 변경
var peerConnection *webrtc.PeerConnection

// 메시지 타입을 명확히 하기 위해 래퍼 구조체 사용
type SignalMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type UDPAddressMessage struct {
	Address string `json:"address"`
}

func main() {
	serverAddr := "ws://solana1000.synology.me:8080/ws" // DDNS 주소 사용
	ws, _, err := websocket.DefaultDialer.Dial(serverAddr, nil)
	if err != nil {
		log.Fatalf("WebSocket 접속 실패: %v", err)
	}
	defer ws.Close()
	log.Println("시그널링 서버에 접속 성공")

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		log.Fatalf("UDP 리스너 시작 실패: %v", err)
	}
	defer udpConn.Close()

	// *** 최종 수정본: 안정적인 포트 추출 ***
	// UDP 주소에서 포트 번호를 안정적인 방법으로 추출합니다.
	udpAddr, ok := udpConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		log.Fatalf("UDP 주소 변환 실패")
	}
	myUdpPort := strconv.Itoa(udpAddr.Port) // 정수형 포트를 문자열로 변환
	log.Printf("내 UDP 리스닝 포트: %s", myUdpPort)

	// 서버에게 내 UDP 포트 번호를 알려줍니다.
	msg := Message{Body: myUdpPort}
	ws.WriteJSON(msg)
	// *** 수정 끝 ***

	go func() {
		for {
			// *** 최종 수정본: 주소 목록 처리 및 지속적인 펀칭 ***
			var receivedAddrs []string
			err := ws.ReadJSON(&receivedAddrs)
			if err != nil {
				log.Println("WebSocket 읽기 에러:", err)
				return
			}

			if len(receivedAddrs) == 0 {
				if otherPeerAddr != nil {
					log.Println("상대 피어가 나갔습니다.")
					otherPeerAddr = nil
				}
				continue
			}

			peerAddrStr := receivedAddrs[0]

			if otherPeerAddr != nil && otherPeerAddr.String() == peerAddrStr {
				continue
			}

			log.Printf("상대 피어 주소 수신: %s", peerAddrStr)
			peerAddr, err := net.ResolveUDPAddr("udp", peerAddrStr)
			if err != nil {
				log.Println("잘못된 UDP 주소:", err)
				continue
			}
			otherPeerAddr = peerAddr

			log.Println("지속적인 UDP 홀 펀칭 시도...")
			go func() {
				for i := 0; i < 10; i++ {
					if otherPeerAddr == nil {
						break
					}
					udpConn.WriteToUDP([]byte("펀칭!"), otherPeerAddr)
					time.Sleep(100 * time.Millisecond)
				}
			}()
			// *** 수정 끝 ***
		}
	}()

	go func() {
		buffer := make([]byte, 1024)
		for {
			n, addr, err := udpConn.ReadFromUDP(buffer)
			if err != nil {
				log.Println("UDP 읽기 에러:", err)
				continue
			}
			log.Printf("UDP 메시지 수신 from %s: %s", addr.String(), string(buffer[:n]))
		}
	}()

	log.Println("채팅 시작 (메시지 입력 후 Enter)")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()
		if otherPeerAddr != nil {
			_, err := udpConn.WriteToUDP([]byte(text), otherPeerAddr)
			if err != nil {
				log.Printf("UDP 메시지 전송 실패: %v", err)
			}
		} else {
			log.Println("아직 상대 피어가 연결되지 않았습니다.")
		}
	}
}
