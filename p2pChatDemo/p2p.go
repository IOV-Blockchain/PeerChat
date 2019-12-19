package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"bytes"
	"encoding/hex"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/rlp"
	"p2pnet/gopkg.in/urfave/cli.v1"
)

var (
	port     int
	bootnode string
)

var (
	hasPeerConnected bool
	node             *p2p.Server
	wg               sync.WaitGroup
)

const (
	statusCode      = uint64(0) // used by whisper protocol
	messagesCode    = uint64(1)
	p2pCode         = uint64(2)
	msgLength       = uint64(64)
	ProtocolVersion = uint64(5)
)

type chat struct {
	protocol p2p.Protocol

	peer map[*peer]struct{}
	PeerMu sync.RWMutex

	msgQueue chan []string
	msgP2p   chan p2pMsg

	quit chan bool
}

type peer struct {
	Chat      *chat
	peers     *p2p.Peer
	readwrite p2p.MsgReadWriter

	quit chan bool
}

type p2pMsg struct{
	peerId  string
	p2pMsg  []string
}


func main() {
	if len(os.Args) < 3 {
		fmt.Println("./app --bootnode staticNodeAddr --port listenPort")
		fmt.Println("1.   --bootnode p2p NetWork need a static node Address to connect")
		fmt.Println("2.   --port Listen port,default port is 10001")
		return
	}

	app := cli.NewApp()
	app.Usage = "p2p package demo"
	app.Action = startP2pNode
	app.Flags = []cli.Flag{
		//命令行解析得到的port
		cli.IntFlag{Name: "port", Value: 10001, Usage: "listen port", Destination: &port},
		//命令行解析得到bootnode
		cli.StringFlag{Name: "bootnode", Value: "", Usage: "boot node", Destination: &bootnode},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func startP2pNode(c *cli.Context) error {
	if args := c.Args();len(args) > 0{
		return fmt.Errorf("invalid command:%v",args[0])
	}

	newChat := NewChat()
	if err := serverConfig(newChat);err!=nil{
		return err
	}


	//node.Start()开启p2p服务
	if err := node.Start(); err != nil {
		return err
	}

	go peerMonitor()

	var quit = make(chan struct{})

	wg.Add(1)
	go newChat.txLoop(quit)

	wg.Wait()

	return nil
}


func serverConfig(newChat *chat) error{
	nodeKey, _ := crypto.GenerateKey()

	node = &p2p.Server{
		Config: p2p.Config{
			MaxPeers:   100,
			PrivateKey: nodeKey,
			Name:       "p2pDemo",
			ListenAddr: fmt.Sprintf(":%d", port),
			Protocols:  []p2p.Protocol{newChat.protocol},
		},
	}

	//从bootnode字符串中解析得到bootNode节点
	bootNode, err := discover.ParseNode(bootnode)
	if err != nil {
		return err
	}

	//p2p服务器从BootstrapNodes中得到相邻节点
	node.Config.BootstrapNodes = []*discover.Node{bootNode}

	return nil
}

func (Chat *chat) txLoop(quit chan struct{}) {
	defer wg.Done()

	for {
		s := readInput()
		if s == "quit()" || s == "exit()" {
			fmt.Println("Program terminated")
			close(quit)
			break
		}
		if len(s) == 0 {
			continue
		}

		if hasPeerConnected {
			Chat.msgSend([]byte(s))
		}

	}
}

func (Chat *chat) msgSend(payload []byte) {
	msg := string(payload)

	Chat.Add(msg)
}

func readInput() string {

	if !hasPeerConnected {
		fmt.Printf("Connecting...(may take several minutes)\n")
	} else {
		fmt.Printf(">>")
	}

	f := bufio.NewReader(os.Stdin)

	input, _ := f.ReadString('\n')
	input = strings.TrimRight(input, "\n\r")

	return input
}

func peerMonitor() {
	subchan := make(chan *p2p.PeerEvent)
	sub := node.SubscribeEvents(subchan)
	defer sub.Unsubscribe()

	for {
		select {
		case v := <-subchan:
			if v.Type == p2p.PeerEventTypeAdd {
				fmt.Printf("\nAdd Peer %s\n", v.Peer.String())
			} else if v.Type == p2p.PeerEventTypeDrop {
				fmt.Printf("\nDrop Peer %s\n", v.Peer.String())
			}

			if node.PeerCount() > 0 {
				hasPeerConnected = true
			} else {
				hasPeerConnected = false
			}
		}
	}
}

func NewChat() *chat {
	newChat := &chat{
		peer:     make(map[*peer]struct{}),
		msgQueue: make(chan []string, 50),
		msgP2p:   make(chan p2pMsg,50),
		quit:     make(chan bool),
	}

	newChat.protocol = p2p.Protocol{
		Name:    "red",
		Version: 2,
		Length:  msgLength,
		Run:     newChat.msgHandler,
	}

	return newChat
}

func (newChar *chat) msgHandler(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
	chatPeer := newPeer(peer, rw)
	newChar.PeerMu.Lock()
	newChar.peer[chatPeer] = struct{}{}
	newChar.PeerMu.Unlock()

	chatPeer.Chat = newChar

	defer func() {
		newChar.PeerMu.Lock()
		delete(newChar.peer, chatPeer)
		newChar.PeerMu.Unlock()
	}()

	if err := chatPeer.handshake(); err != nil {
		return err
	}

	chatPeer.start()
	defer chatPeer.stop()

	return newChar.runMessageLoop(chatPeer, rw)
}

func (Chat *chat) runMessageLoop(Peer *peer, rw p2p.MsgReadWriter) error {
	for {
		packet, err := rw.ReadMsg()

		if err != nil {
			fmt.Println("message loop", "peer", Peer.peers.ID(), "err", err)
			delete(Chat.peer,Peer)
			return err
		}

		switch packet.Code {
		case messagesCode:
			var myMessage []string
			if err := packet.Decode(&myMessage); err != nil {
				fmt.Println("decode msg err", err)
				return err
			}
			fmt.Println("Recv msg:",myMessage)
		case p2pCode:
			var myMessage []string
			if err := packet.Decode(&myMessage); err != nil {
				fmt.Println("decode msg err", err)
				return err
			}
			fmt.Println("Recv p2p msg:",myMessage)

		default:
			fmt.Println("unkown msg code")
		}
	}
}

func (Chat *chat) Add(msg string) error {
	if strings.Contains(msg,"p2p"){
		p2pMsgBuf := strings.Split(msg," ")
		peerId := p2pMsgBuf[1]
		p2pMsgContent := p2pMsgBuf[2]

		msg := p2pMsg{
			peerId:peerId,
			p2pMsg:[]string{p2pMsgContent},
		}

		Chat.msgP2p <- msg

		return nil
	}

	Chat.msgQueue <- []string{msg}

	return nil
}

func (Peer *peer) start() {
	go func() {
		scanTime := time.NewTicker(300 * time.Millisecond)
		for {
			select {
			case <-scanTime.C:
				if err := Peer.broadcast(); err != nil {
					fmt.Println("broadcast failed", "reason", err, "peer", Peer.peers.ID())
					return
				}
			case <-Peer.quit:
				return
			}
		}
	}()

}

func (Peer *peer) stop() {
	Peer.quit <- true
}

func (Peer *peer) broadcast() error {
	Chat := Peer.Chat

	select{
		case e := <-Chat.msgP2p:
			p2pPeer := e.peerId

			p,err := getPeer(p2pPeer,Chat)
			if err != nil{
				fmt.Println("get p2p peer fail!")
				return err
			}

			err = p2p.Send(p.readwrite,p2pCode,e.p2pMsg)
			if err != nil{
				fmt.Println("p2p send message fail!")
				return nil
			}
			return nil

		case e := <-Chat.msgQueue:
			Chat.PeerMu.Lock()
			defer Chat.PeerMu.Unlock()
			for conPeer,_ := range Chat.peer{
				err := p2p.Send(conPeer.readwrite,messagesCode,e)

				if err != nil{
					return err
				}
			}
			return nil

	default:
		return nil
	}

	return nil
}

func getPeer(peerId string,Chat *chat)(*peer,error){
	Chat.PeerMu.Lock()
	defer Chat.PeerMu.Unlock()

	for peer,_ := range Chat.peer{
		id := peer.peers.ID()

		peId,_ := hex.DecodeString(peerId)

		if bytes.Compare(id[:],peId)==0 {
			return peer,nil
		}

	}

	return nil,fmt.Errorf("Could not find peer with ID:%x",peerId)
}

func newPeer(chatPeer *p2p.Peer, rw p2p.MsgReadWriter) *peer {
	return &peer{
		peers:     chatPeer,
		readwrite: rw,
	}
}

func (peer *peer) handshake() error {
	// Send the handshake status message asynchronously
	errc := make(chan error, 1)
	go func() {
		errc <- p2p.Send(peer.readwrite, statusCode, ProtocolVersion)
	}()
	// Fetch the remote status packet and verify protocol match
	packet, err := peer.readwrite.ReadMsg()
	if err != nil {
		fmt.Printf("ReadMsg err:", err)
		return err
	}

	if packet.Code != statusCode {
		fmt.Println("statusCode err")
		return fmt.Errorf("peer [%x] sent packet %x before status packet", peer.peers.ID(), packet.Code)
	}

	s := rlp.NewStream(packet.Payload, uint64(packet.Size))
	peerVersion, err := s.Uint()
	if err != nil {
		return fmt.Errorf("peer [%x] sent bad status message: %v", peer.peers.ID(), err)
	}


	if peerVersion != ProtocolVersion {
		return fmt.Errorf("peer [%x]: protocol version mismatch %d != %d", peer.peers.ID(), peerVersion, ProtocolVersion)
	}
	// Wait until out own status is consumed too
	if err := <-errc; err != nil {
		return fmt.Errorf("peer [%x] failed to send status packet: %v", peer.peers.ID(), err)
	}


	return nil
}
