package gol

import (
	"fmt"
	"log"
	"net/rpc"
	"sync"
	"time"
	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

var quit bool

var sdlUpdated chan bool

var waitSDL sync.WaitGroup

type LiveView struct{}

var sdlCells cellsFlipped

type cellsFlipped struct {
	mu    sync.Mutex
	cells []util.Cell
	turn  int
}

func (c *cellsFlipped) read() ([]util.Cell, int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.cells, c.turn
}

func (c *cellsFlipped) write(new []util.Cell, turns int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cells = new
	c.turn = turns
}

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
	keyPresses <-chan rune
}

func (s *LiveView) TakeInfo(req stubs.LiveRequest, _ *stubs.LiveResponse) error {
	waitSDL.Add(1)
	sdlCells.write(req.Flipped, req.Turn)
	sdlUpdated <- true
	waitSDL.Wait()
	return nil
}

// Create and initialize a new 2D grid with given dimensions
func initializeWorld(height, width int) [][]uint8 {
	world := make([][]uint8, height)
	for i := range world {
		world[i] = make([]uint8, width)
	}
	return world
}

func loadInitialState(p Params, c distributorChannels) [][]uint8 {
	world := initializeWorld(p.ImageHeight, p.ImageWidth)
	filename := fmt.Sprintf("%vx%v", p.ImageWidth, p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			value := <-c.ioInput
			world[x][y] = value

			if value == 255 {

				c.events <- CellFlipped{
					CompletedTurns: 0,
					Cell:           util.Cell{X: x, Y: y},
				}
			}
		}
	}
	c.events <- TurnComplete{0}
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	return world
}

func saveGameState(p Params, c distributorChannels, turns int, world [][]uint8) {
	c.ioCommand <- ioOutput
	filename := fmt.Sprintf("%vx%vx%v", p.ImageWidth, p.ImageHeight, turns)
	c.ioFilename <- filename

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- world[x][y]
		}
	}

	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- ImageOutputComplete{turns, filename}
}

// Send an RPC call to the server and retrieve the updated game state
func executeTurn(client *rpc.Client, req stubs.Request, res *stubs.Response) {
	if err := client.Call(stubs.Turns, req, &res); err != nil {
		fmt.Println(err)
	}
}

func getCount(client *rpc.Client, c distributorChannels) {
	res := new(stubs.ResponseAlive)
	if err := client.Call(stubs.Alive, stubs.EmptyReq{}, &res); err != nil {
		fmt.Println(err)
	}
	c.events <- AliveCellsCount{res.Turn, res.NumAlive}
}

func quitServer(client *rpc.Client) {
	res := stubs.EmptyRes{}
	if err := client.Call(stubs.QuitServer, stubs.EmptyReq{}, &res); err != nil {
		fmt.Println(err)
	}
}

func quitClient(client *rpc.Client) {
	res := stubs.EmptyRes{}
	if err := client.Call(stubs.QuitClient, stubs.EmptyReq{}, &res); err != nil {
		fmt.Println(err)
	}
}

func quitClientPaused(client *rpc.Client) {
	res := stubs.EmptyRes{}
	if err := client.Call(stubs.QuitClientPaused, stubs.EmptyReq{}, &res); err != nil {
		fmt.Println(err)
	}
}

func pauseClient(client *rpc.Client) int {
	res := new(stubs.ResponseTurn)
	if err := client.Call(stubs.Pause, stubs.EmptyReq{}, &res); err != nil {
		fmt.Println(err)
	}
	return res.Turn
}

func unpauseClient(client *rpc.Client) {
	if err := client.Call(stubs.Unpause, stubs.EmptyReq{}, &stubs.EmptyRes{}); err != nil {
		fmt.Println(err)
	}
}

func snapshot(client *rpc.Client, p Params, c distributorChannels) {
	res := new(stubs.ResponseSnapshot)
	if err := client.Call(stubs.Snapshot, stubs.EmptyReq{}, &res); err != nil {
		fmt.Println(err)
	}
	saveGameState(p, c, res.Turns, res.NewWorld)
}

func pausedSnapshot(client *rpc.Client, p Params, c distributorChannels) {
	res := new(stubs.ResponseSnapshot)
	if err := client.Call(stubs.PausedSnapshot, stubs.EmptyReq{}, &res); err != nil {
		fmt.Println(err)
	}
	saveGameState(p, c, res.Turns, res.NewWorld)
}

func runTicker(done chan bool, client *rpc.Client, c distributorChannels) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case _ = <-ticker.C:
			getCount(client, c)

		}
	}
}

func paused(client *rpc.Client, c distributorChannels, p Params) {
	turn := pauseClient(client)
	c.events <- StateChange{turn, Paused}

	for keyNew := range c.keyPresses {
		switch keyNew {
		case 's':
			pausedSnapshot(client, p, c)
		case 'p':
			unpauseClient(client)
			c.events <- StateChange{turn, Executing}
			return
		case 'q':
			quitClientPaused(client)
			quit = true
			return
		}
	}
}

func runKeyPressController(client *rpc.Client, c distributorChannels, p Params) {
	for key := range c.keyPresses {
		switch key {
		case 'k':
			quitServer(client)
			return
		case 's':
			snapshot(client, p, c)
		case 'q':
			quitClient(client)
			quit = true
			return
		case 'p':
			paused(client, c, p)
		}
	}
}

func copyOf(world [][]uint8, p Params) [][]uint8 {
	worldNew := initializeWorld(p.ImageHeight, p.ImageWidth)
	copy(worldNew, world)
	return worldNew
}

func updateSDL(done chan bool, c distributorChannels) {
	for {
		select {
		case <-done:
			return
		case <-sdlUpdated:
			cells, turn := sdlCells.read()
			c.events <- CellsFlipped{turn, cells}
			c.events <- TurnComplete{turn + 1}
			waitSDL.Done()
		}
	}
}

// Manage client-server interaction and distribute work across routines
func distributor(p Params, c distributorChannels, restart bool) {
	serverAddress := "18.170.66.239:8030"
	client, err := rpc.Dial("tcp", serverAddress)
	if err != nil {
		log.Fatal("dialing", err)
	}
	defer client.Close()

	//rpc.Register(&LiveView{})
	//listener, err := net.Listen("tcp", "0.0.0.0:8020")
	//if err != nil {
	//	log.Fatal("Listener error:", err)
	//}
	//log.Println("Server listening on port 8020")
	//defer listener.Close()
	//go rpc.Accept(listener)

	initialWorld := loadInitialState(p, c)
	c.events <- StateChange{0, Executing}

	req := stubs.Request{
		OldWorld:    initialWorld,
		Turns:       p.Turns,
		Threads:     p.Threads,
		Workers:     p.Workers,
		ImageWidth:  p.ImageWidth,
		ImageHeight: p.ImageHeight,
		Restart:     restart,
	}
	res := new(stubs.Response)

	done := make(chan bool)
	sdlUpdated = make(chan bool, 1)
	defer close(done)
	go runTicker(done, client, c)
	go runKeyPressController(client, c, p)
	go updateSDL(done, c)

	executeTurn(client, req, res)

	saveGameState(p, c, res.Turns, copyOf(res.NewWorld, p))
	c.events <- FinalTurnComplete{
		CompletedTurns: res.Turns,
		Alive:          res.AliveCellLocation,
	}

	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- StateChange{res.Turns, Quitting}
	done <- true
	done <- true
	close(c.events)
}
