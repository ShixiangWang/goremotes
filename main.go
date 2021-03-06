package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var version = "1.4"

// init() is called before main()
func init() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: ./gosub <pbs_files_path>\t(version:%s)\n", version)
		fmt.Println("See ./gosub -h for help.")
		os.Exit(-1)
	}
}

// Source: https://flaviocopes.com/go-list-files/
// NOTE: subdirectories will also be visited
func visit(files *[]string, ext string, abs bool) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		if filepath.Ext(path) == ext {
			if abs {
				absPath, err := filepath.Abs(path)
				if err != nil {
					log.Fatal(err)
				}
				*files = append(*files, absPath)
			} else {
				*files = append(*files, path)
			}
		}
		return nil
	}
}

func submit(file string) int {
	log.Printf("Submitting %s\n", file)
	cmd := exec.Command("qsub", file)
	_, err := cmd.CombinedOutput()
	if err != nil {
		//if match, _ := regexp.MatchString("queue limit", err.Error()); match {
		//	log.Println("Found queue limit for submitting job!")
		log.Printf("Submitting %s failed with error:\n", file)
		log.Println(err)
		log.Println("Waiting for 5 minutes..")
		time.Sleep(5 * time.Minute)
		log.Println("Calling back to submit...")
		return submit(file)
		//} else {
		//	log.Fatalf("Submitting %s failed with error:\n[%s]\n", file, err)
		//}
	}

	c := fmt.Sprintf("echo %s >> ./success_submitted_list.txt", file)
	cmd = exec.Command("sh", "-c", c)
	_, err = cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Print file list error: please contact Shixiang.")
	}
	return 0
}

// IsFileExist check if a file exists
// Ref: <https://studygolang.com/topics/20>
func IsFileExist(fileName string) (error, bool) {
	_, err := os.Stat(fileName)
	if err == nil {
		return nil, true
	}
	if os.IsNotExist(err) {
		return nil, false
	}
	return err, false
}

// GenCallPBS check and generate a PBS file
func GenCallPBS(prefix string) string {
	number := 1
	fileName := fmt.Sprintf("./%s%d.pbs", prefix, number)
	_, exists := IsFileExist(fileName)
	for exists {
		log.Printf("File %s exists, trying to set another name.", fileName)
		number = number + 1
		fileName = fmt.Sprintf("./%s%d.pbs", prefix, number)
		_, exists = IsFileExist(fileName)
	}

	log.Printf("Generating file %s.", fileName)
	_, err := os.Create(fileName)
	if err != nil {
		log.Fatal(err)
	} else {
		log.Printf("Created file %s.", fileName)
	}
	return fileName
}

// https://learnku.com/articles/36203
func removeDuplicateElement(languages []string) []string {
	result := make([]string, 0, len(languages))
	temp := map[string]struct{}{}
	for _, item := range languages {
		if _, ok := temp[item]; !ok {
			temp[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

func main() {
	pPtr := flag.Bool("p", false, "enable parallel processing.")
	nodePtr := flag.Int("nodes", 1, "an int to specify node number to use. Only work when -p enabled.")
	ppnPtr := flag.Int("ppn", 1, "an int to specify cpu number per node. Only work when -p enabled.")
	jobPtr := flag.Int("jobs", 0, "run n jobs in parallel, at default will use nodes*ppn. Only work when -p enabled.")
	memPtr := flag.String("mem", "auto", "memory size, e.g. 5gb. Only work when -p enabled.")
	walltimePtr := flag.String("walltime", "24:00:00", "walltime setting. Only work when -p enabled.")
	outPtr := flag.String("name", "pwork", "an file prefix for generating output PBS file. Only work when -p enabled.")
	holdPtr := flag.Bool("hold", false, "set it if you want to check and qsub by your own. Only work when -p enabled.")
	absPtr := flag.Bool("abs", false, "render and use PBS absolute path.")

	flag.Parse()
	inPath := flag.Args()

	//fmt.Println(*pPtr, "-", *nodePtr, "-", *ppnPtr, "-", *outPtr, "-", inPath)
	nodes := *nodePtr
	ppns := *ppnPtr
	jobs := *jobPtr

	log.Printf("gosub version: %s\n", version)
	log.Println("Submitted file list will be save to success_submitted_list.txt!")
	log.Println("====================================")

	// Remove previous file
	if _, exists := IsFileExist("./success_submitted_list.txt"); exists {
		log.Println("Previous file success_submitted_list.txt detected, removing it...")
		cmd := exec.Command("sh", "-c", "rm ./success_submitted_list.txt")
		_, err := cmd.CombinedOutput()

		if err != nil {
			log.Fatalf("Remove previous file error: please contact Shixiang.")
		}
	}

	// List all PBS files
	var files []string
	for _, f := range inPath {
		err := filepath.Walk(f, visit(&files, ".pbs", *absPtr))
		if err != nil {
			log.Fatal(err)
		}
	}
	// Make sure no duplicate files
	files = removeDuplicateElement(files)
	log.Println("Detected files: ", files)

	if len(files) == 0 {
		log.Fatalf("No pbs files found in directory %s!", inPath[0])
	}

	if *pPtr {
		// Run parallel mode
		log.Println("Parallel mode is enabled.")
		log.Println("====================================")
		info := fmt.Sprintf("Use %d threads: %d CPUs per Node.", nodes*ppns, *ppnPtr)
		log.Println(info)
		pbs := GenCallPBS(*outPtr)

		// Generate a work pbs script
		// Use the first file as template
		// Generate header
		// Then generate run commands
		cmd1 := "echo '#PBS -N gosub_parallel_work' >> " + pbs
		cmd2 := fmt.Sprintf("echo '#PBS -l nodes=%d:ppn=%d' >> %s", nodes, ppns, pbs)
		cmd3 := fmt.Sprintf("echo '#PBS -l walltime=%s' >> %s", *walltimePtr, pbs)
		cmd4 := fmt.Sprintf("cat %s | grep '#PBS' | grep -v nodes | grep -v '#PBS -N' | grep -v 'walltime' | grep -v 'mem'>> %s", files[0], pbs)

		// Write parallel computation commands to generated file
		// Doc: https://github.com/shenwei356/rush
		totalJobs := 0
		if jobs == 0 {
			totalJobs = nodes * ppns
		} else {
			totalJobs = jobs
		}
		fileStr := strings.Join(files, " ")
		log.Println("Joined file list with spaces:", fileStr)
		cmdP := fmt.Sprintf("echo \"echo %s | rush -D ' ' 'bash {}' -j %d\" >> %s", fileStr, totalJobs, pbs)

		cmds := make([]string, 6)
		if *memPtr == "auto" {
			cmds = append(cmds, cmd1, cmd2, cmd3, cmd4, cmdP)
		} else {
			// Mem setting
			cmd5 := fmt.Sprintf("echo '#PBS -l mem=%s' >> %s", *memPtr, pbs)
			cmds = append(cmds, cmd1, cmd2, cmd3, cmd5, cmd4, cmdP)
		}

		for _, c := range cmds {
			cmd := exec.Command("sh", "-c", c)
			_, err := cmd.CombinedOutput()
			if err != nil {
				log.Fatal(err)
			}
		}

		log.Println("NOTE the 'rush' command should be available in PATH.")
		if *holdPtr {
			log.Println("A 'hold' command is detected, please check the generated pbs and modify it if necessary before you qsub it.")
		} else {
			// Submit this work script
			submit(pbs)
		}

	} else {
		// Submit PBS one by one
		for _, file := range files {
			submit(file)
		}
	}

	log.Println("====================================")
	log.Println("End.")
}
