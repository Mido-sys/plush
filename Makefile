test:
	go test -failfast -race -short -cover ./...
	go mod tidy -v

cov:
	go test -short -coverprofile cover.out ./...
	go tool cover -html cover.out
	go mod tidy -v

.PHONY: benchmark-strings-builder benchmark-output-estimator \
	benchmark-render-engines profile-output-estimator

OUTPUT_ESTIMATOR_PROFILE_DIR ?= /tmp/plush-output-estimator-profiles
OUTPUT_ESTIMATOR_PROFILE_BENCHTIME ?= 10s

benchmark-strings-builder:
	GOMAXPROCS=1 go test ./vm/vm -run '^$$' -bench '^Benchmark_StringsBuilder_OutputSizePlanning/' -benchmem -count=5 -benchtime=500ms

benchmark-output-estimator:
	GOMAXPROCS=1 go test ./vm/vm -run '^$$' -bench '^Benchmark_VM_Output_Size_Estimator/' -benchmem -count=3 -benchtime=500ms

benchmark-render-engines:
	GOMAXPROCS=1 go test ./vm/vm -run '^$$' -bench '^Benchmark_Heavy_Template_Render_Engine/' -benchmem -count=5 -benchtime=500ms

profile-output-estimator:
	mkdir -p '$(OUTPUT_ESTIMATOR_PROFILE_DIR)'
	GOMAXPROCS=1 go test ./vm/vm -run '^$$' -bench '^Benchmark_VM_Output_Size_Estimator/stable/disabled$$' -benchtime='$(OUTPUT_ESTIMATOR_PROFILE_BENCHTIME)' -count=1 -cpuprofile='$(OUTPUT_ESTIMATOR_PROFILE_DIR)/disabled.cpu.pprof' -memprofile='$(OUTPUT_ESTIMATOR_PROFILE_DIR)/disabled.mem.pprof'
	GOMAXPROCS=1 go test ./vm/vm -run '^$$' -bench '^Benchmark_VM_Output_Size_Estimator/stable/enabled_diagnostics_off$$' -benchtime='$(OUTPUT_ESTIMATOR_PROFILE_BENCHTIME)' -count=1 -cpuprofile='$(OUTPUT_ESTIMATOR_PROFILE_DIR)/enabled.cpu.pprof' -memprofile='$(OUTPUT_ESTIMATOR_PROFILE_DIR)/enabled.mem.pprof'
	rm -f vm.test
