package proxy

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"time"
    "strings"
    "math/rand"

	"github.com/shengupiao/open-ethereum-pool/util"
)

const (
	MaxReqSize = 1024
)

func (s *ProxyServer) ListenTCP() {
	timeout := util.MustParseDuration(s.config.Proxy.Stratum.Timeout)
	s.timeout = timeout

	addr, err := net.ResolveTCPAddr("tcp", s.config.Proxy.Stratum.Listen)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	server, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	defer server.Close()

	log.Printf("Stratum listening on %s", s.config.Proxy.Stratum.Listen)
	var accept = make(chan int, s.config.Proxy.Stratum.MaxConn)
	n := 0

	for {
		conn, err := server.AcceptTCP()
		if err != nil {
			continue
		}
		conn.SetKeepAlive(true)

		ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

		if s.policy.IsBanned(ip) || !s.policy.ApplyLimitPolicy(ip) {
			conn.Close()
			continue
		}
		n += 1
		cs := &Session{conn: conn, ip: ip}

		accept <- n
		go func(cs *Session) {
			err = s.handleTCPClient(cs)
			if err != nil {
				s.removeSession(cs)
				conn.Close()
			}
			<-accept
		}(cs)
	}
}

func (s *ProxyServer) handleTCPClient(cs *Session) error {
	cs.enc = json.NewEncoder(cs.conn)
	connbuff := bufio.NewReaderSize(cs.conn, MaxReqSize)
	s.setDeadline(cs.conn)

	for {
		data, isPrefix, err := connbuff.ReadLine()
		if isPrefix {
			log.Printf("Socket flood detected from %s", cs.ip)
			s.policy.BanClient(cs.ip)
			return err
		} else if err == io.EOF {
			log.Printf("Client %s disconnected", cs.ip)
			s.removeSession(cs)
			break
		} else if err != nil {
			log.Printf("Error reading from socket: %v", err)
			return err
		}

		if len(data) > 1 {
			var req StratumReq
			err = json.Unmarshal(data, &req)
			if err != nil {
				s.policy.ApplyMalformedPolicy(cs.ip)
				log.Printf("Malformed stratum request from %s: %v", cs.ip, err)
				return err
			}
			s.setDeadline(cs.conn)
			err = cs.handleTCPMessage(s, &req)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func addHexPrefix(str string) string {
    if len(str) < 2 || str[:2] == "0x" {
        return str
    }
    return "0x" + str
}

func removeHexPrefix(str string) string {
    if len(str) < 2 || str[:2] != "0x"  {
        return str
    }
    return str[2:]
}

func (cs *Session) handleTCPMessage(s *ProxyServer, req *StratumReq) error {
	// Handle RPC methods
	switch req.Method {
	case "mining.subscribe":
        cs.protocolType = "stratum_nicehash"
		var params []string
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			log.Println("Malformed stratum request params from", cs.ip)
			return err
		}

        if params[1] != "EthereumStratum/1.0.0"{
            log.Println("Unsupported stratum version from ", cs.ip)
            return cs.sendTCPNHError(req.Id, []interface{}{
                -1,
                "unsupported ethereum version",
                nil,
            })
        }
        t := s.currentBlockTemplate()
        jobID := generateRandomString(8)
        extranonce := generateRandomString(4)
        cs.JobDeatils = jobDetails{
            JobID: jobID,
            Extranonce: extranonce,
            SeedHash: removeHexPrefix(t.Seed),
            HeaderHash: removeHexPrefix(t.Header),
        }

        result := []interface{}{
            []string{
                "mining.notify",
                jobID,
                "EthereumStratum/1.0.0",
            },
            extranonce,
        }

        resp := JSONRpcResp{
            Id:req.Id,
            Version:"EthereumStratum/1.0.0",
            Result:result,
            Error: nil,
        }
        return cs.sendTCPNHResult(resp)

	case "mining.authorize":
        cs.protocolType = "stratum_nicehash"
		var params []string
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			return errors.New("invalid params")
		}
		splitData := strings.Split(params[0], ".")
		params[0] = addHexPrefix(splitData[0])

		reply , errReply := s.handleLoginRPC(cs, params, req.Worker)
		if errReply != nil {
            return cs.sendTCPNHError(req.Id, []interface{}{
                errReply.Code,
                errReply.Message,
                nil,
            })
		}

		resp := JSONRpcResp{Id:req.Id, Result:reply, Error:nil}
		if err := cs.sendTCPNHResult(resp); err != nil{
			return err
		}

		paramsDiff := []float64{
			float64(s.config.Proxy.Difficulty) / 4295032833,
		}
		respReq := JSONRpcReqNH{Method:"mining.set_difficulty", Params:paramsDiff}
		if err := cs.sendTCPNHReq(respReq); err != nil {
			return err
		}

		return cs.sendJob(s, req.Id)
	case "mining.submit":
        cs.protocolType = "stratum_nicehash"
		var params []string
		if err := json.Unmarshal(*req.Params, &params); err != nil{
			return err
		}

		splitData := strings.Split(params[0], ".")
		id := splitData[0]
        if id[:2] != "0x" {
            id = "0x" + id
        }

		if cs.JobDeatils.JobID != params[1] {
            var errorArray []interface{}
            errorArray = append(errorArray, -1)
            errorArray = append(errorArray, "wrong job id")
            errorArray = append(errorArray, nil)
			return cs.sendTCPNHError(req.Id, errorArray)
		}
		nonce := cs.JobDeatils.Extranonce + params[2]

        if nonce[:2] != "0x" {
            nonce = "0x" + nonce
        }

        seedHash := cs.JobDeatils.SeedHash
        if seedHash[:2] != "0x" {
            seedHash = "0x" + seedHash
        }

        headerHash := cs.JobDeatils.HeaderHash
        if headerHash[:2] != "0x" {
            headerHash = "0x" + headerHash
        }

		params = []string{
			nonce,
            headerHash,
            "",
		}

		reply, errReply := s.handleTCPSubmitRPC(cs, id, params)
		if errReply != nil {
            var errorArray []interface{}
            errorArray = append(errorArray, errReply.Code)
            errorArray = append(errorArray, errReply.Message)
            errorArray = append(errorArray, nil)
			return cs.sendTCPNHError(req.Id, errorArray)
		}
		resp := JSONRpcResp{
			Id: req.Id,
			Result: reply,
		}

		if err := cs.sendTCPNHResult(resp); err != nil{
			return err
		}

		return cs.sendJob(s, req.Id)

    case "mining.extranonce.subscribe":
        cs.protocolType = "stratum_nicehash"
		var params []string
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			return errors.New("invalid params")
		}

        return cs.sendExtranonce(s, req.Id)

	case "eth_submitLogin":
        cs.protocolType = "stratum"
		var params []string
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			log.Println("Malformed stratum request params from", cs.ip)
			return err
		}
		reply, errReply := s.handleLoginRPC(cs, params, req.Worker)
		if errReply != nil {
			return cs.sendTCPError(req.Id, errReply)
		}
		return cs.sendTCPResult(req.Id, reply)
	case "eth_getWork":
        cs.protocolType = "stratum"
		reply, errReply := s.handleGetWorkRPC(cs)
		if errReply != nil {
			return cs.sendTCPError(req.Id, errReply)
		}
		return cs.sendTCPResult(req.Id, &reply)
	case "eth_submitWork":
        cs.protocolType = "stratum"
		var params []string
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			log.Println("Malformed stratum request params from", cs.ip)
			return err
		}
		reply, errReply := s.handleTCPSubmitRPC(cs, req.Worker, params)
		if errReply != nil {
			return cs.sendTCPError(req.Id, errReply)
		}
		return cs.sendTCPResult(req.Id, &reply)
	case "eth_submitHashrate":
        cs.protocolType = "stratum"
		return cs.sendTCPResult(req.Id, true)
	default:
        if req.Method[:6] == "mining" {
            var errorArray []interface{}
            errorArray = append(errorArray, -1)
            errorArray = append(errorArray, "unknown method")
            errorArray = append(errorArray, nil)
		    return cs.sendTCPNHError(req.Id, errorArray)
        } else {
		    errReply := s.handleUnknownRPC(cs, req.Method)
		    return cs.sendTCPError(req.Id, errReply)
        }
	}
}

func (cs *Session) sendTCPResult(id *json.RawMessage, result interface{}) error {
	cs.Lock()
	defer cs.Unlock()

	message := JSONRpcResp{Id: id, Version: "2.0", Error: nil, Result: result}
	return cs.enc.Encode(&message)
}

func (cs *Session) pushNewJob(result interface{}) error {
	cs.Lock()
	defer cs.Unlock()
	// FIXME: Temporarily add ID for Claymore compliance
	message := JSONPushMessage{Version: "2.0", Result: result, Id: 0}
	return cs.enc.Encode(&message)
}

func (cs *Session) sendTCPError(id *json.RawMessage, reply *ErrorReply) error {
	cs.Lock()
	defer cs.Unlock()

	message := JSONRpcResp{Id: id, Version: "2.0", Error: reply}
	err := cs.enc.Encode(&message)
	if err != nil {
		return err
	}
	return errors.New(reply.Message)
}

func (self *ProxyServer) setDeadline(conn *net.TCPConn) {
	conn.SetDeadline(time.Now().Add(self.timeout))
}

func (s *ProxyServer) registerSession(cs *Session) {

	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.sessions[cs] = struct{}{}
}

func (s *ProxyServer) removeSession(cs *Session) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	delete(s.sessions, cs)
}

func (s *ProxyServer) broadcastNewJobs() {
	t := s.currentBlockTemplate()
	if t == nil || len(t.Header) == 0 || s.isSick() {
		return
	}
	reply := []string{t.Header, t.Seed, s.diff}

	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()

	count := len(s.sessions)
	log.Printf("Broadcasting new job to %v stratum miners", count)

	start := time.Now()
	bcast := make(chan int, 1024)
	n := 0

	for m, _ := range s.sessions {
        if m.protocolType != "stratum" {
            continue
        }
		n++
		bcast <- n

		go func(cs *Session) {
			err := cs.pushNewJob(&reply)
			<-bcast
			if err != nil {
				log.Printf("Job transmit error to %v@%v: %v", cs.login, cs.ip, err)
				s.removeSession(cs)
			} else {
				s.setDeadline(cs.conn)
			}
		}(m)
	}
	log.Printf("Jobs broadcast finished %s", time.Since(start))
}

func(cs *Session) sendTCPNHError(id *json.RawMessage, message interface{}) error{
    cs.Mutex.Lock()
    defer cs.Mutex.Unlock()
    resp := JSONRpcResp{Id: id, Error: message}
    return cs.enc.Encode(&resp)
}

func(cs *Session) sendTCPNHResult(resp JSONRpcResp)  error {
    cs.Mutex.Lock()
    defer cs.Mutex.Unlock()
    return cs.enc.Encode(&resp)
}

func(cs *Session) sendTCPNHReq(resp JSONRpcReqNH)  error {
    cs.Mutex.Lock()
    defer cs.Mutex.Unlock()
    return cs.enc.Encode(&resp)
}

func(cs *Session) sendJob(s *ProxyServer, id *json.RawMessage) error {
	resp := JSONRpcReqNH{
		Method:"mining.notify",
		Params: []interface{}{
			cs.JobDeatils.JobID,
			cs.JobDeatils.SeedHash,
			cs.JobDeatils.HeaderHash,
			true,
		},
	}

	return cs.sendTCPNHReq(resp)
}

func (cs *Session) sendExtranonce(s *ProxyServer, id *json.RawMessage) error {
    resp := JSONRpcReqNH{
        Method:"mining.set_extranonce",
        Params: []interface{}{
            cs.JobDeatils.Extranonce,
        },
    }
    return cs.sendTCPNHReq(resp)
}

func (s *ProxyServer) broadcastNewJobsNH() {
	t := s.currentBlockTemplate()
	if t == nil || len(t.Header) == 0 || s.isSick() {
		return
	}

	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()

	count := len(s.sessions)
	log.Printf("Broadcasting new nice hash job to %v stratum nice hash  miners", count)

	start := time.Now()
	bcast := make(chan int, 1024)
	n := 0

	for m, _ := range s.sessions {
        if m.protocolType != "stratum_nicehash" {
            continue
        }
		n++
		bcast <- n

        seedHash := t.Seed
        headerHash := t.Header
        if seedHash[:2] == "0x" {
            seedHash = seedHash[2:]
        }
        if headerHash[:2] == "0x" {
            headerHash = headerHash[2:]
        }

		go func(cs *Session) {
            job := jobDetails{
				JobID: generateRandomString(8),
				SeedHash: seedHash,
				HeaderHash: headerHash,
                Extranonce: cs.JobDeatils.Extranonce,
            }

			cs.JobDeatils = job

			resp := JSONRpcReqNH{
				Method:"mining.notify",
				Params: []interface{}{
					cs.JobDeatils.JobID,
					cs.JobDeatils.SeedHash,
					cs.JobDeatils.HeaderHash,
					true,
				},
			}

			err := cs.sendTCPNHReq(resp)
			<-bcast
			if err != nil {
				log.Printf("Job transmit error to %v@%v: %v", cs.login, cs.ip, err)
				s.removeSession(cs)
			} else {
				s.setDeadline(cs.conn)
			}
		}(m)
	}
	log.Printf("Nice hash jobs broadcast finished %s", time.Since(start))
}

func generateRandomString(strlen int) string {
	rand.Seed(time.Now().UTC().UnixNano())
	const chars = "abcdef0123456789"
	result := make([]byte, strlen)
	for i := 0; i < strlen; i++ {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}
