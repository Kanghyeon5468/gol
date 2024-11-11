package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"sync"
	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

var nextWorker *rpc.Client

var lowerBoundGive, lowerBoundGet []int

var wg, calculatingLowerBound, receivingLowerBound sync.WaitGroup

type ParallelNode struct{}

type pixel struct {
	X     int
	Y     int
	Value uint8
}

var quitting = make(chan bool, 1)

func (n *ParallelNode) GetSegment(req stubs.ParallelWorkerRequest, res *stubs.ParallelWorkerResponse) error {
	res.Segment, res.Flipped = calculateNewAliveParallel(req.Threads, req.WholeWorld, req.Start, req.End, req.Size)
	return nil
}

func (n *ParallelNode) Quit(_ stubs.WorkerRequest, _ *stubs.WorkerResponse) error {
	quitting <- true
	return nil
}

func (n *ParallelNode) ConnectToNextWorker(req stubs.AddressReq, _ *stubs.EmptyRes) error {
	worker, err := rpc.Dial("tcp", req.Address)
	if err != nil {
		return err
	}
	nextWorker = worker
	return nil
}

func (n *ParallelNode) AcceptPartialRows(req *stubs.HaloRowsReq, res *stubs.HaloRowsRes) error {
	lowerBoundGet = req.Row
	receivingLowerBound.Done()
	calculatingLowerBound.Wait()
	calculatingLowerBound.Add(1)
	res.Row = lowerBoundGive
	return nil
}

func giveRowsWorker(partialRow []int) []int {
	res := new(stubs.HaloRowsRes)
	req := stubs.HaloRowsReq{
		Row: partialRow,
	}
	if err := nextWorker.Call(stubs.PartialRows, &req, res); err != nil {
		fmt.Println(err)
	}
	return res.Row
}

func calculateNewAliveParallel(workerNum int, world [][]uint8, start, end, worldSize int) ([][]uint8, []util.Cell) {
	//numRows := p.ImageHeight / workerNum

	splitSegments := make([]chan pixel, workerNum)
	for i := range splitSegments {
		splitSegments[i] = make(chan pixel, worldSize*worldSize)
	}
	// start workers to make the world
	//startWorkers(workerNum, numRows, p, dataChan, neighbourChan, c)
	setupWorkers(worldSize, start, end, workerNum, splitSegments, world, start == 0, end == worldSize)

	var cells []util.Cell

	wg.Wait()
	for i := 0; i < workerNum; i++ {
		length := len(splitSegments[i])
		for j := 0; j < length; j++ {
			item := <-splitSegments[i]
			world[item.X][item.Y] = item.Value
			cells = append(cells, util.Cell{X: item.X, Y: item.Y})
		}
	}

	return world[start:end], cells
}
func setupWorkers(worldHeight, sectionStart, sectionEnd, workerNum int, splitSegments []chan pixel, world [][]uint8, starting, ending bool) {
	sectionHeight := sectionEnd - sectionStart
	numRows := sectionHeight / workerNum
	//if numRows == 0 {
	//	numRows = 1
	//}

	// you are one worker calculating the entire world
	if starting && ending {
		wg.Add(1)
		// no halo needed
		go runWorker(worldHeight, 0, sectionHeight, splitSegments[0], world, true, true)
		return
	}

	if workerNum == 1 {
		wg.Add(1)
		go runWorkerHaloBoth(worldHeight, sectionStart, sectionStart+numRows, splitSegments[0], world, starting, ending)
		return
	}

	wg.Add(1)
	go runWorkerHaloStart(worldHeight, sectionStart, sectionStart+numRows, splitSegments[0], world, starting, false)

	i := 1
	for i < workerNum-1 {
		wg.Add(1)
		go runWorker(worldHeight, sectionStart+i*numRows, sectionStart+(i+1)*numRows, splitSegments[i], world, false, false)
		i++
	}

	if workerNum > 1 {
		// final worker does the remaining rows
		wg.Add(1)
		go runWorkerHaloEnd(worldHeight, sectionStart+i*numRows, sectionStart+sectionHeight, splitSegments[i], world, false, ending)
	}
}

func runWorker(worldHeight, start, end int, splitSegment chan pixel, world [][]uint8, starting, ending bool) {
	defer wg.Done()
	neighboursWorld := calculateNeighbours(start, end, worldHeight, world, starting, ending)
	calculateNextWorld(start, end, worldHeight, splitSegment, world, neighboursWorld)
}

func runWorkerHaloBoth(worldHeight, start, end int, splitSegment chan pixel, world [][]uint8, starting, ending bool) {
	defer wg.Done()
	neighboursWorld := calculateNeighboursHaloBoth(start, end, worldHeight, world, starting, ending)
	calculateNextWorld(start, end, worldHeight, splitSegment, world, neighboursWorld)
}

func runWorkerHaloStart(worldHeight, start, end int, splitSegment chan pixel, world [][]uint8, starting, ending bool) {
	defer wg.Done()
	neighboursWorld := calculateNeighboursHaloStart(start, end, worldHeight, world, starting, ending)
	calculateNextWorld(start, end, worldHeight, splitSegment, world, neighboursWorld)
}

func runWorkerHaloEnd(worldHeight, start, end int, splitSegment chan pixel, world [][]uint8, starting, ending bool) {
	defer wg.Done()
	neighboursWorld := calculateNeighboursHaloEnd(start, end, worldHeight, world, starting, ending)
	calculateNextWorld(start, end, worldHeight, splitSegment, world, neighboursWorld)
}

func calculateNextWorld(start, end, width int, c chan pixel, world [][]uint8, neighboursWorld [][]int) {
	for y := start; y < end; y++ {
		for x := 0; x < width; x++ {
			neighbors := neighboursWorld[y][x]
			if (neighbors < 2 || neighbors > 3) && world[y][x] == 255 {
				c <- pixel{y, x, 0}
			} else if neighbors == 3 && world[y][x] == 0 {
				c <- pixel{y, x, 255}
			}
		}
	}
}

func calculateNeighbours(start, end, width int, world [][]uint8, starting, ending bool) [][]int {
	neighbours := make([][]int, width)
	for i := range neighbours {
		neighbours[i] = make([]int, width)
	}

	if !(starting && ending) {
		if starting {
			for x := 0; x < width; x++ {
				if world[width-1][x] == 255 {
					for i := -1; i <= 1; i++ {
						//for image wrap around
						xCoord := x + i
						if xCoord < 0 {
							xCoord = width - 1
						} else if xCoord >= width {
							xCoord = 0
						}
						neighbours[0][xCoord]++
					}
				}
			}
		} else {
			start--
		}

		if ending {
			for x := 0; x < width; x++ {
				if world[0][x] == 255 {
					for i := -1; i <= 1; i++ {
						//for image wrap around
						xCoord := x + i
						if xCoord < 0 {
							xCoord = width - 1
						} else if xCoord >= width {
							xCoord = 0
						}
						neighbours[width-1][xCoord]++
					}
				}
			}
		} else {
			end++
		}
	}

	for y := start; y < end; y++ {
		for x := 0; x < width; x++ {
			// if a cell is 255
			if world[y][x] == 255 {
				// add 1 to all neighbours
				// i and j are the offset
				for i := -1; i <= 1; i++ {
					for j := -1; j <= 1; j++ {

						// if you are not offset, do not add one. This is yourself
						if !(i == 0 && j == 0) {
							ny, nx := y+i, x+j
							if nx < 0 {
								nx = width - 1
							} else if nx == width {
								nx = 0
							} else {
								nx = nx % width
							}

							if ny < 0 {
								ny = width - 1
							} else if ny == width {
								ny = 0
							} else {
								ny = ny % width
							}

							neighbours[ny][nx]++
						}

					}
				}
			}
		}
	}

	return neighbours
}

func calculateNeighboursHaloBoth(start, end, width int, world [][]uint8, starting, ending bool) [][]int {
	neighbours := make([][]int, width)
	for i := range neighbours {
		neighbours[i] = make([]int, width)
	}

	lowerBoundGive = make([]int, width)

	for x := 0; x < width; x++ {
		// if a cell is 255
		if world[start][x] == 255 {
			// add 1 to all neighbours
			// i and j are the offset
			for i := -1; i <= 1; i++ {
				for j := -1; j <= 1; j++ {

					// if you are not offset, do not add one. This is yourself
					if !(i == 0 && j == 0) {
						ny, nx := start+i, x+j
						if nx < 0 {
							nx = width - 1
						} else if nx == width {
							nx = 0
						} else {
							nx = nx % width
						}

						if ny < 0 {
							ny = width - 1
						} else if ny == width {
							ny = 0

						} else {
							ny = ny % width
						}

						neighbours[ny][nx]++
						if i == -1 {
							lowerBoundGive[nx]++
						}
					}

				}
			}
		}
	}
	calculatingLowerBound.Done()

	upper := make([]int, width)
	for x := 0; x < width; x++ {
		// if a cell is 255
		if world[end-1][x] == 255 {
			// add 1 to all neighbours
			// i and j are the offset
			for i := -1; i <= 1; i++ {
				for j := -1; j <= 1; j++ {

					// if you are not offset, do not add one. This is yourself
					if !(i == 0 && j == 0) {
						ny, nx := end-1+i, x+j
						if nx < 0 {
							nx = width - 1
						} else if nx == width {
							nx = 0
						} else {
							nx = nx % width
						}

						if ny < 0 {
							ny = width - 1
						} else if ny == width {
							ny = 0
						} else {
							ny = ny % width
						}

						neighbours[ny][nx]++
						if i == 1 {
							upper[nx]++
						}
					}

				}
			}
		}
	}
	extra := giveRowsWorker(upper)
	for i, item := range extra {
		neighbours[end-1][i] += item
	}

	for y := start + 1; y < end-1; y++ {
		for x := 0; x < width; x++ {
			// if a cell is 255
			if world[y][x] == 255 {
				// add 1 to all neighbours
				// i and j are the offset
				for i := -1; i <= 1; i++ {
					for j := -1; j <= 1; j++ {

						// if you are not offset, do not add one. This is yourself
						if !(i == 0 && j == 0) {
							ny, nx := y+i, x+j
							if nx < 0 {
								nx = width - 1
							} else if nx == width {
								nx = 0
							} else {
								nx = nx % width
							}

							if ny < 0 {
								ny = width - 1
							} else if ny == width {
								ny = 0
							} else {
								ny = ny % width
							}

							neighbours[ny][nx]++
						}

					}
				}
			}
		}
	}

	receivingLowerBound.Wait()
	receivingLowerBound.Add(1)
	for i, item := range lowerBoundGet {
		neighbours[start][i] += item
	}

	//if width == 16 {
	//	for i, item := range neighbours {
	//		fmt.Println(i, item)
	//	}
	//}
	return neighbours
}

func calculateNeighboursHaloStart(start, end, width int, world [][]uint8, starting, ending bool) [][]int {
	neighbours := make([][]int, width)
	for i := range neighbours {
		neighbours[i] = make([]int, width)
	}

	lowerBoundGive = make([]int, width)

	for x := 0; x < width; x++ {
		// if a cell is 255
		if world[start][x] == 255 {
			// add 1 to all neighbours
			// i and j are the offset
			for i := -1; i <= 1; i++ {
				for j := -1; j <= 1; j++ {

					// if you are not offset, do not add one. This is yourself
					if !(i == 0 && j == 0) {
						ny, nx := start+i, x+j
						if nx < 0 {
							nx = width - 1
						} else if nx == width {
							nx = 0
						} else {
							nx = nx % width
						}

						if ny < 0 {
							ny = width - 1
						} else if ny == width {
							ny = 0

						} else {
							ny = ny % width
						}

						neighbours[ny][nx]++
						if i == -1 {
							lowerBoundGive[nx]++
						}
					}

				}
			}
		}
	}
	calculatingLowerBound.Done()

	if ending {
		for x := 0; x < width; x++ {
			if world[0][x] == 255 {
				for i := -1; i <= 1; i++ {
					//for image wrap around
					xCoord := x + i
					if xCoord < 0 {
						xCoord = width - 1
					} else if xCoord >= width {
						xCoord = 0
					}
					neighbours[width-1][xCoord]++
				}
			}
		}
	} else {
		end++
	}

	for y := start + 1; y < end; y++ {
		for x := 0; x < width; x++ {
			// if a cell is 255
			if world[y][x] == 255 {
				// add 1 to all neighbours
				// i and j are the offset
				for i := -1; i <= 1; i++ {
					for j := -1; j <= 1; j++ {

						// if you are not offset, do not add one. This is yourself
						if !(i == 0 && j == 0) {
							ny, nx := y+i, x+j
							if nx < 0 {
								nx = width - 1
							} else if nx == width {
								nx = 0
							} else {
								nx = nx % width
							}

							if ny < 0 {
								ny = width - 1
							} else if ny == width {
								ny = 0
							} else {
								ny = ny % width
							}

							neighbours[ny][nx]++
						}

					}
				}
			}
		}
	}

	receivingLowerBound.Wait()
	receivingLowerBound.Add(1)
	for i, item := range lowerBoundGet {
		neighbours[start][i] += item
	}

	//if width == 16 {
	//	for i, item := range neighbours {
	//		fmt.Println(i, item)
	//	}
	//}
	return neighbours
}

func calculateNeighboursHaloEnd(start, end, width int, world [][]uint8, starting, ending bool) [][]int {
	neighbours := make([][]int, width)
	for i := range neighbours {
		neighbours[i] = make([]int, width)
	}

	upper := make([]int, width)
	for x := 0; x < width; x++ {
		// if a cell is 255
		if world[end-1][x] == 255 {
			// add 1 to all neighbours
			// i and j are the offset
			for i := -1; i <= 1; i++ {
				for j := -1; j <= 1; j++ {

					// if you are not offset, do not add one. This is yourself
					if !(i == 0 && j == 0) {
						ny, nx := end-1+i, x+j
						if nx < 0 {
							nx = width - 1
						} else if nx == width {
							nx = 0
						} else {
							nx = nx % width
						}

						if ny < 0 {
							ny = width - 1
						} else if ny == width {
							ny = 0
						} else {
							ny = ny % width
						}

						neighbours[ny][nx]++
						if i == 1 {
							upper[nx]++
						}
					}

				}
			}
		}
	}
	extra := giveRowsWorker(upper)
	for i, item := range extra {
		neighbours[end-1][i] += item
	}

	if starting {
		for x := 0; x < width; x++ {
			if world[width-1][x] == 255 {
				for i := -1; i <= 1; i++ {
					//for image wrap around
					xCoord := x + i
					if xCoord < 0 {
						xCoord = width - 1
					} else if xCoord >= width {
						xCoord = 0
					}
					neighbours[0][xCoord]++
				}
			}
		}
	} else {
		start--
	}

	for y := start; y < end-1; y++ {
		for x := 0; x < width; x++ {
			// if a cell is 255
			if world[y][x] == 255 {
				// add 1 to all neighbours
				// i and j are the offset
				for i := -1; i <= 1; i++ {
					for j := -1; j <= 1; j++ {

						// if you are not offset, do not add one. This is yourself
						if !(i == 0 && j == 0) {
							ny, nx := y+i, x+j
							if nx < 0 {
								nx = width - 1
							} else if nx == width {
								nx = 0
							} else {
								nx = nx % width
							}

							if ny < 0 {
								ny = width - 1
							} else if ny == width {
								ny = 0
							} else {
								ny = ny % width
							}

							neighbours[ny][nx]++
						}

					}
				}
			}
		}
	}

	//if width == 16 {
	//	for _, item := range neighbours {
	//		fmt.Println(item)
	//	}
	//}
	return neighbours
}

func main() {
	serverPort := flag.String("port", "8040", "Port to Listen")
	flag.Parse()

	receivingLowerBound.Add(1)
	calculatingLowerBound.Add(1)

	rpc.Register(&ParallelNode{})
	listener, err := net.Listen("tcp", "0.0.0.0:"+*serverPort)
	if err != nil {
		log.Fatal("Listener error:", err)
	}
	log.Println("Server listening on port", *serverPort)
	defer listener.Close()
	go rpc.Accept(listener)

	_ = <-quitting
}
