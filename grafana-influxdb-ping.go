package main

import (
	"bufio"
	"fmt"
	"github.com/coreos/go-systemd/daemon"
	"github.com/influxdata/influxdb/client/v2"
	"github.com/pelletier/go-toml"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	
)

const (
	configFile = "/etc/grafana/influxdb/ping/config.toml"
)

func aerr(err error){
	if err != nil {
        fmt.Println("Error:", err.Error())
        os.Exit(1)
    }
}

func berr(err error){
	if err != nil {
        log.Fatal(err)
    }
}

func slashSplitter(c rune) bool {
    return c == '/'
}

func influxDBClient(url string, username string, password string) client.Client {
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     url,
		Username: username,
		Password: password,
	})
	aerr(err)
	return c
}

func getData(probeName string, c client.Client, database string, measurement string, args []string){
	cmd := exec.Command("/usr/bin/fping", args...)
	stdout, err := cmd.StdoutPipe()
    aerr(err)
    stderr, err := cmd.StderrPipe()
    aerr(err)
    cmd.Start()
    berr(err)
	buff := bufio.NewScanner(stderr)
    for buff.Scan() {
        text := buff.Text()
        fields := strings.Fields(text)
        if len(fields) > 1 {
            host := fields[0]
            data := fields[4]
            dataSplitted := strings.FieldsFunc(data, slashSplitter)
            dataSplitted[2] = strings.TrimRight(dataSplitted[2], "%,")
            sent, recv, lossp := dataSplitted[0], dataSplitted[1], dataSplitted[2]
            min, max, avg := "", "", ""
            if len(fields) > 5 {
                times := fields[7]
                td := strings.FieldsFunc(times, slashSplitter)
                min, avg, max = td[0], td[1], td[2]
            }
            log.Printf("Host:%s, loss: %s, min: %s, avg: %s, max: %s, sent: %s, recv: %s", host, lossp, min, avg, max, sent, recv)
            createMetrics(probeName, c, database, measurement, host, sent, recv, lossp, min, avg, max)
        }
    }
	std := bufio.NewReader(stdout)
    line, err := std.ReadString('\n')
    berr(err)
    log.Printf("stdout:%s", line)
}

func createMetrics(probeName string, c client.Client, database string, measurement string, host string, sent string, recv string, lossp string, min string, avg string, max string){
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  database,
		Precision: "s",
	})
	berr(err)
	
	eventTime := time.Now().Add(time.Second * -20)
	loss, _ := strconv.Atoi(lossp)
	min2, _ := strconv.ParseFloat(min, 64)
    avg2, _ := strconv.ParseFloat(avg, 64)
    max2, _ := strconv.ParseFloat(max, 64)
    tags := map[string]string{
		"probe": probeName,
		"host": host,
	}
	fields := map[string]interface{}{
		"ping_loss": loss,
		"min": min2,
		"max": max2,
		"avg": avg2,
	}
	point, err := client.NewPoint(
		measurement,
		tags,
		fields,
		eventTime.Add(time.Second*10),
	)
	if err != nil {
		log.Fatalln("Error: ", err)
	}

	bp.AddPoint(point)
	err = c.Write(bp)
	if err != nil {
		log.Fatal(err)
	}
	daemon.SdNotify(false, "READY=1")
}

func main(){
	config, err := toml.LoadFile(configFile)
	aerr(err)
	
	probeName    := config.Get("influxdb.probe_name").(string)
	address      := config.Get("influxdb.address").(string)
	database     := config.Get("influxdb.database").(string)
	measurement  := config.Get("influxdb.measurement").(string)
	username     := config.Get("influxdb.username").(string)
	password     := config.Get("influxdb.password").(string)
	retry        := config.Get("options.retry").(string)
	summary      := config.Get("options.summary").(string)
	interval     := config.Get("options.interval").(string)
	hosts	     := config.Get("hosts.hosts").([]interface{})
	args 	     := []string{"-B 1", "-D", retry, "-O 0", summary, interval, "-l"}
	
	for _, v := range hosts {
        host, _ := v.(string)
        args = append(args, host)
    }
	
	log.Printf("Try to connect at %s with %s/%s", address, username, password)
	c := influxDBClient(address, username, password)
	getData(probeName, c, database, measurement, args)
}