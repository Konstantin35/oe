package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	"github.com/mcarloai/open-ethereum-pool/storage"
	"github.com/mcarloai/open-ethereum-pool/util"
)

type ApiConfig struct {
	Enabled              bool   `json:"enabled"`
	Listen               string `json:"listen"`
	StatsCollectInterval string `json:"statsCollectInterval"`
	HashrateWindow       string `json:"hashrateWindow"`
	HashrateLargeWindow  string `json:"hashrateLargeWindow"`
	HashrateLittleWindow string `json:"hashrateLittleWindow"`
	LuckWindow           []int  `json:"luckWindow"`
	Payments             int64  `json:"payments"`
	Blocks               int64  `json:"blocks"`
	PurgeOnly            bool   `json:"purgeOnly"`
	PurgeInterval        string `json:"purgeInterval"`
	SaleStatsInterval    string `json:"saleStatsInterval"`
	HashrateInterval     string `json:"hashrateInterval"`
}

type ApiServer struct {
	config               *ApiConfig
	backend              *storage.RedisClient
	hashrateWindow       time.Duration
	hashrateLargeWindow  time.Duration
	hashrateLittleWindow time.Duration
	stats                atomic.Value
	saleStats            atomic.Value
	miners               map[string]*Entry
	minersMu             sync.RWMutex
	statsIntv            time.Duration
}

type Entry struct {
	stats     map[string]interface{}
	updatedAt int64
}

func NewApiServer(cfg *ApiConfig, backend *storage.RedisClient) *ApiServer {
	hashrateWindow := util.MustParseDuration(cfg.HashrateWindow)
	hashrateLargeWindow := util.MustParseDuration(cfg.HashrateLargeWindow)
	hashrateLittleWindow := util.MustParseDuration(cfg.HashrateLittleWindow)
	return &ApiServer{
		config:               cfg,
		backend:              backend,
		hashrateWindow:       hashrateWindow,
		hashrateLargeWindow:  hashrateLargeWindow,
		hashrateLittleWindow: hashrateLittleWindow,
		miners:               make(map[string]*Entry),
	}
}

func (s *ApiServer) Start() {
	if s.config.PurgeOnly {
		log.Printf("Starting API in purge-only mode")
	} else {
		log.Printf("Starting API on %v", s.config.Listen)
	}

	s.statsIntv = util.MustParseDuration(s.config.StatsCollectInterval)
	statsTimer := time.NewTimer(s.statsIntv)
	log.Printf("Set stats collect interval to %v", s.statsIntv)

	purgeIntv := util.MustParseDuration(s.config.PurgeInterval)
	purgeTimer := time.NewTimer(purgeIntv)
	log.Printf("Set purge interval to %v", purgeIntv)

	saleIntv := util.MustParseDuration(s.config.SaleStatsInterval)
	saleTimer := time.NewTimer(saleIntv)
	log.Printf("Set sale interval to %v", saleIntv)

	hashrateIntv := util.MustParseDuration(s.config.HashrateInterval)
	hashrateTimer := time.NewTimer(hashrateIntv)
	log.Printf("Set hashrate interval to %v", hashrateIntv)

	sort.Ints(s.config.LuckWindow)

	if s.config.PurgeOnly {
		s.purgeStale()
	} else {
		s.purgeStale()
		s.collectStats()
		s.getSaleStats()
	}

	go func() {
		for {
			select {
			case <-statsTimer.C:
				if !s.config.PurgeOnly {
					s.collectStats()
				}
				statsTimer.Reset(s.statsIntv)
			case <-saleTimer.C:
				if !s.config.PurgeOnly {
					s.getSaleStats()
				}
				saleTimer.Reset(saleIntv)
			case <-purgeTimer.C:
				s.purgeStale()
				purgeTimer.Reset(purgeIntv)
			case <-hashrateTimer.C:
				s.collectHashrate()
				hashrateTimer.Reset(hashrateIntv)
			}
		}
	}()

	if !s.config.PurgeOnly {
		s.listen()
	}
}

func (s *ApiServer) listen() {
	r := mux.NewRouter()
	r.HandleFunc("/api/stats", s.StatsIndex)
	r.HandleFunc("/api/miners", s.MinersIndex)
	r.HandleFunc("/api/blocks", s.BlocksIndex)
	r.HandleFunc("/api/payments", s.PaymentsIndex)
	r.HandleFunc("/api/accounts/{login:0x[0-9a-fA-F]{40}}", s.AccountIndex)
	r.HandleFunc("/api/sales", s.SalesIndex)
	r.NotFoundHandler = http.HandlerFunc(notFound)
	err := http.ListenAndServe(s.config.Listen, r)
	if err != nil {
		log.Fatalf("Failed to start API: %v", err)
	}
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusNotFound)
}

func (s *ApiServer) purgeStale() {
	start := time.Now()
	total, err := s.backend.FlushStaleStats(s.hashrateWindow, s.hashrateLargeWindow)
	if err != nil {
		log.Println("Failed to purge stale data from backend:", err)
	} else {
		log.Printf("Purged stale stats from backend, %v shares affected, elapsed time %v", total, time.Since(start))
	}
}

func (s *ApiServer) collectStats() {
	start := time.Now()
	stats, err := s.backend.CollectStats(s.hashrateWindow, s.config.Blocks, s.config.Payments)
	if err != nil {
		log.Printf("Failed to fetch stats from backend: %v", err)
		return
	}
	if len(s.config.LuckWindow) > 0 {
		stats["luck"], err = s.backend.CollectLuckStats(s.config.LuckWindow)
		if err != nil {
			log.Printf("Failed to fetch luck stats from backend: %v", err)
			return
		}
	}

	s.stats.Store(stats)
	log.Printf("Stats collection finished %s", time.Since(start))
}

func (s *ApiServer) collectHashrate() {
	for key, miner := range s.miners {
		s.backend.SaveMinerHashrate(miner.stats["currentHashrate"].(int64), key)
	}
}

func (s *ApiServer) StatsIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	nodes, err := s.backend.GetNodeStates()
	if err != nil {
		log.Printf("Failed to get nodes stats from backend: %v", err)
	}
	reply["nodes"] = nodes

	stats := s.getStats()
	if stats != nil {
		reply["now"] = util.MakeTimestamp()
		reply["stats"] = stats["stats"]
		reply["hashrate"] = stats["hashrate"]
		reply["minersTotal"] = stats["minersTotal"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
	}

	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) MinersIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	stats := s.getStats()
	if stats != nil {
		reply["now"] = util.MakeTimestamp()
		reply["miners"] = stats["miners"]
		reply["hashrate"] = stats["hashrate"]
		reply["minersTotal"] = stats["minersTotal"]
	}

	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) BlocksIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	stats := s.getStats()
	if stats != nil {
		reply["matured"] = stats["matured"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["immature"] = stats["immature"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["candidates"] = stats["candidates"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
		reply["luck"] = stats["luck"]
	}

	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) PaymentsIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	stats := s.getStats()
	if stats != nil {
		reply["payments"] = stats["payments"]
		reply["paymentsTotal"] = stats["paymentsTotal"]
	}

	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) AccountIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	login := strings.ToLower(mux.Vars(r)["login"])
	s.minersMu.Lock()
	defer s.minersMu.Unlock()

	reply, ok := s.miners[login]
	now := util.MakeTimestamp()
	cacheIntv := int64(s.statsIntv / time.Millisecond)
	// Refresh stats if stale
	if !ok || reply.updatedAt < now-cacheIntv {
		exist, err := s.backend.IsMinerExists(login)
		if !exist {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}

		stats, err := s.backend.GetMinerStats(login, s.config.Payments)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}
		workers, err := s.backend.CollectWorkersStats(s.hashrateWindow, s.hashrateLargeWindow, s.hashrateLittleWindow, login)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}
		for key, value := range workers {
			stats[key] = value
		}
		stats["pageSize"] = s.config.Payments

		shareRatio := s.backend.GetShareRatio(login)
		if len(shareRatio) == 4 {
			stats["acceptedShare"] = shareRatio[0]
			stats["staleShare"] = shareRatio[1]
			stats["duplicateShare"] = shareRatio[2]
			stats["invalidShare"] = shareRatio[3]
		}

		shareChart, err := s.backend.GetShareChart(login)
		if err == nil {
			stats["shareChart"] = shareChart
		} else {
			log.Printf("Failed to fetch shareChart from backend: %v", err)
		}

		graphHashrate, err := s.backend.GetHashrateChart(login)
		if err == nil {
			stats["graphHr"] = graphHashrate
		} else {
			log.Printf("Failed to fetch hashrateChart from backend: %v", err)
		}

		reply = &Entry{stats: stats, updatedAt: now}
		s.miners[login] = reply
	}

	nodes, err := s.backend.GetNodeStates()
	if err != nil {
		log.Printf("Failed to get nodes stats from backend: %v", err)
	}
	reply.stats["difficulty"] = nodes[0]["difficulty"]

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply.stats)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) getStats() map[string]interface{} {
	stats := s.stats.Load()
	if stats != nil {
		return stats.(map[string]interface{})
	}
	return nil
}

func (s *ApiServer) SalesIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	ss := s.saleStats.Load()
	saleStats := make(map[string]interface{})
	if ss != nil {
		saleStats = ss.(map[string]interface{})
	}

	err := json.NewEncoder(w).Encode(saleStats)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) getSaleStats() {
	start := time.Now()

	stats := s.getStats()
	if stats == nil {
		log.Printf("Stats null")
		return
	}

	// calc hashrate by sales
	if _, ok := stats["miners"]; ok {

		sale2HR := make(map[string]int64)
		sale2THR := make(map[string]int64)
		saleStats := make(map[string]interface{})

		for id, _ := range stats["miners"].(map[string]storage.Miner) {
			workerStats, err := s.backend.CollectWorkersStats(s.hashrateWindow, s.hashrateLargeWindow, s.hashrateLittleWindow, id)
			if err != nil {
				log.Printf("Failed to fetch sales stats from backend: %v, id: %v", err, id)
				continue
			}

			if _, ok := workerStats["workers"]; ok {
				for id, worker := range workerStats["workers"].(map[string]storage.Worker) {
					fields := strings.SplitN(id, "_", 2)
					saleId := "MinerBabe"
					if len(fields) == 2 {
						saleId = fields[0]
					}

					if _, ok := sale2HR[saleId]; ok {
						sale2HR[saleId] = sale2HR[saleId] + worker.HR
					} else {
						sale2HR[saleId] = worker.HR
					}

					if _, ok := sale2THR[saleId]; ok {
						sale2THR[saleId] = sale2THR[saleId] + worker.TotalHR
					} else {
						sale2THR[saleId] = worker.TotalHR
					}
				}
			}
		}

		for saleId, _ := range sale2HR {
			s := make(map[string]int64)
			s["hr"] = sale2HR[saleId]
			s["thr"] = sale2THR[saleId]
			saleStats[saleId] = s
		}
		s.saleStats.Store(saleStats)
	}

	log.Printf("Get sale stats finished %s", time.Since(start))
}
