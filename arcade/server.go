package arcade

import (
	"arcade/arcade/message"
	"arcade/arcade/net"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xtaci/kcp-go/v5"
)

const timeoutInterval = 2500 * time.Millisecond
const heartbeatInterval = 250 * time.Millisecond
const rttAverageNum = 10

type ConnectedClientInfo struct {
	LastHeartbeat      time.Time
	HeartbeatSendTimes map[int]time.Time
	RTTs               []time.Duration
}

func (c *ConnectedClientInfo) GetMeanRTT() time.Duration {
	var sum time.Duration
	count := 0

	for i := len(c.RTTs) - 1; i >= 0 && i >= len(c.RTTs)-(rttAverageNum+1); i-- {
		sum += c.RTTs[i]
		count++
	}

	if count == 0 {
		return -1 * time.Millisecond
	}

	return sum / time.Duration(count)
}

type Server struct {
	sync.RWMutex

	Network *net.Network

	Addr string
	ID   string

	connectedClients map[string]*ConnectedClientInfo

	// Message IDs to ping times
	pingMessageTimes map[string]time.Time
}

// NewServer creates the server with a given address.
func NewServer(addr string, port int) *Server {
	log.Println("new server", addr)
	id := uuid.NewString()
	net := net.NewNetwork(id, port)

	s := &Server{
		Addr:             addr,
		Network:          net,
		ID:               id,
		connectedClients: make(map[string]*ConnectedClientInfo),
		pingMessageTimes: make(map[string]time.Time),
	}

	go s.startHeartbeats()
	return s
}

func (s *Server) startHeartbeats() {
	for {
		s.Lock()

		for clientID, info := range s.connectedClients {
			client, ok := s.Network.GetClient(clientID)

			if !ok || time.Since(info.LastHeartbeat) >= timeoutInterval {
				arcade.ViewManager.ProcessEvent(NewClientDisconnectEvent(clientID))
				delete(s.connectedClients, clientID)
				continue
			}

			metadata := arcade.ViewManager.GetHeartbeatMetadata()

			client.Lock()
			s.Network.Send(client, NewHeartbeatMessage(client.Seq, metadata))
			s.connectedClients[clientID].HeartbeatSendTimes[client.Seq] = time.Now()
			client.Seq++
			client.Unlock()
		}

		s.Unlock()

		<-time.After(heartbeatInterval)
	}
}

func (s *Server) BeginHeartbeats(clientID string) {
	s.Lock()
	defer s.Unlock()

	s.connectedClients[clientID] = &ConnectedClientInfo{
		LastHeartbeat:      time.Now(),
		HeartbeatSendTimes: make(map[int]time.Time),
		RTTs:               make([]time.Duration, 0),
	}
}

func (s *Server) EndHeartbeats(clientID string) {
	s.Lock()
	defer s.Unlock()

	delete(s.connectedClients, clientID)
}

func (s *Server) EndAllHeartbeats() {
	s.Lock()
	defer s.Unlock()

	s.connectedClients = make(map[string]*ConnectedClientInfo)
}

func (s *Server) GetHeartbeatClients() map[string]*ConnectedClientInfo {
	s.RLock()
	defer s.RUnlock()

	return s.connectedClients
}

func (s *Server) handleMessage(client interface{}, msg interface{}) {
	c := client.(*net.Client)

	baseMsg := reflect.ValueOf(msg).FieldByName("Message").Interface().(message.Message)

	// Signal message received if necessary
	s.Network.SignalReceived(baseMsg.MessageID, msg)

	if arcade.Distributor {
		fmt.Println(msg)
		fmt.Printf("Received '%s' from %s\n", baseMsg.Type, baseMsg.SenderID[:4])

		if baseMsg.Type == "error" {
			fmt.Println(msg)
		}
	}

	// Process message and prepare response
	var res interface{}

	switch msg := msg.(type) {
	case DisconnectMessage:
		arcade.ViewManager.ProcessEvent(&ClientDisconnectEvent{
			ClientID: c.ID,
		})

		s.Network.Disconnect(c.ID)
	default:
		if baseMsg.RecipientID != s.ID {
			if arcade.Distributor {
				fmt.Println("Forwarding message to", baseMsg.RecipientID[:4])
				fmt.Println(msg)
			}

			s.RLock()
			recipient, ok := s.Network.GetClient(baseMsg.RecipientID)
			s.RUnlock()

			if ok {
				s.Network.Send(recipient, msg)
				return
			} else {
				res = NewErrorMessage("Invalid recipient")
			}
		} else {
			if arcade.Distributor {
				fmt.Println(msg)
				panic("Recipient: " + baseMsg.RecipientID + ", self: " + s.ID)
			}

			switch msg := msg.(type) {
			case HeartbeatMessage:
				s.Lock()
				if _, ok := s.connectedClients[msg.SenderID]; ok {
					s.connectedClients[msg.SenderID].LastHeartbeat = time.Now()
					c.Distance = float64(s.connectedClients[msg.SenderID].GetMeanRTT().Milliseconds())
				}
				s.Unlock()

				// Send heartbeat metadata to view
				arcade.ViewManager.ProcessEvent(NewHeartbeatEvent(msg.Metadata))

				// Reply to heartbeat
				res = NewHeartbeatReplyMessage(msg.Seq)
			case HeartbeatReplyMessage:
				if msg.RecipientID == s.ID {
					s.Lock()
					if _, ok := s.connectedClients[msg.SenderID]; ok {
						s.connectedClients[msg.SenderID].LastHeartbeat = time.Now()
						s.connectedClients[msg.SenderID].RTTs = append(s.connectedClients[msg.SenderID].RTTs, time.Since(s.connectedClients[msg.SenderID].HeartbeatSendTimes[msg.Seq]))
					}
					s.Unlock()

					arcade.ViewManager.RequestDebugRender()
				}
			default:
				res = ProcessMessage(c, msg)
			}
		}
	}

	if res == nil {
		return
	} else if err, ok := res.(error); ok {
		res = NewErrorMessage(err.Error())
	}

	// Set sender and recipient IDs
	reflect.ValueOf(res).Elem().FieldByName("Message").FieldByName("RecipientID").Set(reflect.ValueOf(baseMsg.SenderID))
	reflect.ValueOf(res).Elem().FieldByName("Message").FieldByName("SenderID").Set(reflect.ValueOf(s.ID))

	// Set message ID if there was one in the sent packet
	reflect.ValueOf(res).Elem().FieldByName("Message").FieldByName("MessageID").Set(reflect.ValueOf(baseMsg.MessageID))

	s.Network.Send(c, res)
}

// startServer starts listening for connections on a given address.
func (s *Server) start() error {
	log.Println("listening", s.Addr)
	listener, err := kcp.Listen(s.Addr)

	if err != nil {
		panic(err)
	}

	fmt.Printf("Listening at %s...\n", s.Addr)
	fmt.Printf("ID: %s\n", s.ID)

	go listenMulticast()

	for {
		// Wait for new client connections
		conn, err := listener.Accept()

		if err != nil {
			panic(err)
		}

		log.Println("new conn!", conn.LocalAddr().String(), conn.RemoteAddr().String())
		s.Network.Connect(conn.RemoteAddr().String(), conn)
	}
}
