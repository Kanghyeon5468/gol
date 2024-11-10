package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"sync"
	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

var distWorkerNum int

var workerCount int

var quitting = make(chan bool, 1)

type RestartInfo struct {
	restart bool
	turns   int
	world   [][]uint8
}

type BoolContainer struct {
	mu     sync.Mutex
	status bool
}

type IntContainer2 struct {
	mu    sync.Mutex
	value int
	turn  int
}

type IntContainer struct {
	mu   sync.Mutex
	turn int
}

type WorldContainer struct {
	mu    sync.Mutex
	world [][]uint8
	turn  int
}

type LargeWorldContainer struct {
	mu     sync.Mutex
	worlds [][][]uint8
}

func (c *LargeWorldContainer) setAtIndex(i int, w [][]uint8) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.worlds[i] = w
}

func (c *LargeWorldContainer) getAtIndex(i int) [][]uint8 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.worlds[i]
}

func (c *LargeWorldContainer) setup(u [][][]uint8) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.worlds = u
}

func (c *BoolContainer) get() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.status
}

func (c *BoolContainer) setTrue() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.status = true
}

func (c *BoolContainer) setFalse() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.status = false
}

func (c *IntContainer2) getTurn() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.turn
}

func (c *IntContainer) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.turn
}

func (c *IntContainer) set(val int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.turn = val
}

func (c *IntContainer2) getCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.value
}

func (w *WorldContainer) getWorld() [][]uint8 {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.world
}
func (w *WorldContainer) getTurn() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.turn
}

func (w *WorldContainer) set(new [][]uint8, turn int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.world = new
	w.turn = turn
}

func (w *WorldContainer) setWorld(new [][]uint8) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.world = new
}

func (w *WorldContainer) setTurn(new int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.turn = new
}

func (c *IntContainer2) set(turnsCompleted, count int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.value = count
	c.turn = turnsCompleted
}

var pausedTurn IntContainer
var getCount, paused, snapShot, quit, shut, clientQuit BoolContainer
var aliveCount IntContainer2
var waitingSnapShot, pause, waitingForCount, waitingPause, waitingShut, makingSnapshot, working sync.WaitGroup
var world, snapshotInfo WorldContainer
var restartInformation RestartInfo
var splitWorld LargeWorldContainer

func getAliveCells(height, width int, world [][]uint8) []util.Cell {
	aliveCells := make([]util.Cell, 0)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if world[x][y] == 255 {
				aliveCells = append(aliveCells, util.Cell{X: x, Y: y})
			}
		}
	}
	return aliveCells
}

func getAliveCellsFor(world [][]uint8, height, width int) int {
	count := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if world[x][y] == 255 {
				count++
			}
		}
	}
	return count
}

type Server struct{}

func (s *Server) GetAliveCells(_ stubs.RequestAlive, res *stubs.ResponseAlive) error {
	getCount.setTrue()
	waitingForCount.Add(1)
	waitingForCount.Wait()
	res.NumAlive = aliveCount.getCount()
	res.Turn = aliveCount.getTurn()
	return nil
}

func (s *Server) GetSnapshot(_ stubs.RequestAlive, res *stubs.ResponseSnapshot) error {
	// we want to get back the state of the board
	snapShot.setTrue()
	waitingSnapShot.Add(1)
	waitingSnapShot.Wait()
	res.Turns = snapshotInfo.getTurn()
	res.NewWorld = snapshotInfo.getWorld()
	return nil
}

func (s *Server) GetSnapshotPaused(_ stubs.RequestAlive, res *stubs.ResponseSnapshot) error {
	// we want to get back the state of the board
	makingSnapshot.Add(1)
	res.NewWorld = world.getWorld()
	res.Turns = world.turn
	makingSnapshot.Done()
	return nil
}

func (s *Server) PauseProcessing(_ stubs.EmptyReq, res *stubs.ResponseTurn) error {
	// pause the processing
	//waitingPause.Add(1)
	pause.Add(1)
	paused.setTrue()
	//waitingPause.Wait()
	res.Turn = pausedTurn.get()
	return nil
}
func (s *Server) Quit(_ stubs.EmptyReq, _ *stubs.EmptyRes) error {
	// quit once next turn is complete
	quit.setTrue()
	return nil
}

func (s *Server) ClientQuit(_ stubs.EmptyReq, _ *stubs.EmptyRes) error {
	clientQuit.setTrue()
	return nil
}

func (s *Server) ClientQuitPause(_ stubs.EmptyReq, _ *stubs.EmptyRes) error {
	clientQuit.setTrue()
	pause.Done()
	return nil
}

func (s *Server) UnpauseProcessing(_ stubs.EmptyReq, _ *stubs.EmptyRes) error {
	paused.setFalse()
	pause.Done()
	return nil
}
func (s *Server) ShutDistribute() error {
	shut.setTrue()
	waitingShut.Add(1)
	waitingShut.Wait()
	return nil
}

func makeNewWorld(height, width int) [][]uint8 {
	newWorld := make([][]uint8, height)
	for i := range newWorld {
		newWorld[i] = make([]uint8, width)
	}
	return newWorld
}

func runWorker(client *rpc.Client, size, start, end int, currentWorld [][]uint8, splitSegment chan [][]uint8, splitFlipped chan []util.Cell) {
	res := new(stubs.WorkerResponse)
	req := stubs.WorkerRequest{
		WholeWorld: currentWorld,
		Start:      start,
		End:        end,
		Size:       size,
	}
	if err := client.Call(stubs.CalculateWorldSegment, req, res); err != nil {
		fmt.Println(err)
	}
	splitSegment <- res.Segment
	// splitFlipped <- res.Flipped
}

func setupWorkers(workers []*rpc.Client, size, workerNum int, currentWorld [][]uint8, splitSegments []chan [][]uint8, splitFlipped []chan []util.Cell) {
	numRows := size / workerNum

	i := 0
	for i < workerNum-1 {
		go runWorker(workers[i], size, i*numRows, numRows*(i+1), currentWorld, splitSegments[i], splitFlipped[i])
		i++
	}

	// final worker does the remaining rows
	go runWorker(workers[i], size, i*numRows, size, currentWorld, splitSegments[i], splitFlipped[i])
}

func closeWorkers(workers []*rpc.Client) {
	req := new(stubs.WorkerRequest)
	res := new(stubs.WorkerResponse)
	for _, worker := range workers {
		if err := worker.Call(stubs.End, req, &res); err != nil {
			fmt.Println(err)
		}
		worker.Close()
	}
}

// func giveFlipped(sdlClient *rpc.Client, flipped []util.Cell, turn int) {
// 	req := stubs.LiveRequest{Flipped: flipped, Turn: turn}
// 	res := stubs.EmptyRes{}
// 	if err := sdlClient.Call(stubs.GiveInfo, req, &res); err != nil {
// 		fmt.Println(err)
// 	}
// }

func dialWorkers(workerNum int) []*rpc.Client {
	// serverAddress := "127.0.0.1"
	// workerPorts := [8]string{":8040", ":8050", ":8060", ":8070", ":8080", ":8090", ":9000", ":9010"}
	workerPrivateAddress := [8]string{"172.31.24.115:8040", "172.31.18.64:8050", "172.31.31.193:8060", "172.31.21.74:8070", "172.31.17.226:8080", "172.31.26.116:8090", "172.31.26.101:9000","172.31.25.71:9010"}
	workers := make([]*rpc.Client, workerNum)

	for i := 0; i < workerNum; i++ {
		worker, err := rpc.Dial("tcp", fmt.Sprintf("%v%v", workerPrivateAddress[i]))
		// worker, err := rpc.Dial("tcp", workerPrivateAddress[i])
		if err != nil {
			fmt.Println(err)
		}
		workers[i] = worker
	}

	return workers
}

func calculateNextWorld(workers []*rpc.Client, currentWorld [][]uint8, size, workerNum int) ([][]uint8, []util.Cell) {
	var newWorld [][]uint8
	var flipped []util.Cell

	splitSegments := make([]chan [][]uint8, workerNum)
	splitFlipped := make([]chan []util.Cell, workerNum)
	for i := range splitSegments {
		splitSegments[i] = make(chan [][]uint8)
		splitFlipped[i] = make(chan []util.Cell)
	}

	setupWorkers(workers, size, workerNum, currentWorld, splitSegments, splitFlipped)
	// no wait group needed as channel waits for worker to finish
	for i := 0; i < workerNum; i++ {
		newWorld = append(newWorld, <-splitSegments[i]...)
		flipped = append(flipped, <-splitFlipped[i]...)
	}

	//closeWorkers(workers)
	return newWorld, flipped
}

// func dialSdl() *rpc.Client {
// 	sdlAddress := "127.0.0.1:8020"
// 	worker, err := rpc.Dial("tcp", sdlAddress)
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	return worker
// }

func (s *Server) ProcessTurns(req stubs.Request, res *stubs.Response) error {
	var flipped []util.Cell
	currentWorld := req.OldWorld
	nextWorld := makeNewWorld(req.ImageHeight, req.ImageWidth)
	turn := 0
	if req.Restart {
		if restartInformation.restart {
			currentWorld = restartInformation.world
			turn = restartInformation.turns
		} else {
			return errors.New("nothing to restart with")
		}
	}
	workers := dialWorkers(distWorkerNum)
	// sdlClient := dialSdl()

	for turnNum := 0; turnNum < req.Turns; turnNum++ {
		// 매 턴마다 nextWorld를 새롭게 계산
		nextWorld, flipped = calculateNextWorld(workers, currentWorld, req.ImageWidth, distWorkerNum)

		// 결과를 응답 구조체에 설정
		//res.AliveCell = getNumAliveCells(req.ImageHeight, req.ImageWidth, nextWorld)
		turn = turnNum + 1

		// 다음 턴을 위해 world 교체
		currentWorld = nextWorld
		world.set(currentWorld, turn)
		// giveFlipped(sdlClient, flipped, turn)

		makingSnapshot.Wait()

		//fmt.Println(turn)
		if paused.get() {
			pausedTurn.set(turn)
			//waitingPause.Done()
			pause.Wait()
		}
		if quit.get() {
			break
		}
		if snapShot.get() {
			snapShot.setFalse()
			snapshotInfo.set(currentWorld, turn)
			waitingSnapShot.Done()
		}
		if getCount.get() {
			getCount.setFalse()
			aliveCount.set(turn, getAliveCellsFor(currentWorld, req.ImageHeight, req.ImageWidth))
			waitingForCount.Done()
		}
		if clientQuit.get() {
			restartInformation = RestartInfo{restart: true, turns: turn, world: currentWorld}
			clientQuit.setFalse()
			break
		}
	}

	res.Turns = turn
	res.NewWorld = currentWorld
	res.AliveCellLocation = getAliveCells(req.ImageHeight, req.ImageWidth, currentWorld)

	if quit.get() {
		quitting <- true
	}
	return nil
}

func main() {
	serverPort := flag.String("port", "8030", "Port to Listen")
	workers := flag.Int("threads", 1, "How many workers to use")
	flag.Parse()

	distWorkerNum = *workers

	rpc.Register(&Server{})
	listener, err := net.Listen("tcp", "0.0.0.0:"+*serverPort)
	if err != nil {
		log.Fatal("Listener error:", err)
	}
	log.Println("Server listening on port", *serverPort)
	defer listener.Close()
	go rpc.Accept(listener)

	_ = <-quitting
}
