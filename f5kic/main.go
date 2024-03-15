package main

func main() {
	setupCmdFlags()
	electAsLeader()
	setupGlobals()
	setupPrometheus()
	setupRevealer()
	setupCallbacks()

	go ltmWorker()
	go startManagers()
	go httpListenServe()

	nilLoop()
}
