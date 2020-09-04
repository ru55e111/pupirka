package main

import (
	"encoding/json"
	"fmt"
	"github.com/gammazero/workerpool"
	logrus "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"time"
)

type Logger logrus.Logger

func ScanDevice() {
	dir := ConfigV.GetString("path.devices")

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		es := fmt.Sprintf(err.Error())
		LogConsole.Error(es)
		LogConsole.Error("Program exit")
		os.Exit(1)
	}

	for _, f := range files {
		fr := regexp.MustCompile(`\.json$`)
		if frr := string(fr.Find([]byte(f.Name()))); frr == "" {
			es := fmt.Sprintf("File %s skipped, not valid file extension", f.Name())
			LogConsole.Error(es)
			continue
		}
		DeviceFiles = append(DeviceFiles, f.Name())
	}
}

func ReadDevice(Dev *DeviceList) {
	for _, f := range DeviceFiles {
		filepath := fmt.Sprintf("%s/%s", ConfigV.GetString("path.devices"), f)
		jsonFile, err := os.Open(filepath)
		if err != nil {
			es := fmt.Sprintf("Error file Open %s, Error:%s, Skip file", f, err.Error())
			LogConsole.Error(es)
			continue
		}
		defer jsonFile.Close()

		byteValueFromFile, err := ioutil.ReadAll(jsonFile)
		if err != nil {

			es := fmt.Sprintf("Error file Read %s, Error:%s, Skip file", f, err.Error())
			LogConsole.Error(es)
			jsonFile.Close()
			continue
		}
		var d Device
		err = json.Unmarshal(byteValueFromFile, &d)
		if err != nil {
			es := fmt.Sprintf("Error Read file json  %s, Error:%s, Skip file", f, err.Error())
			LogConsole.Error(es)
			jsonFile.Close()
			continue
		}
		d.Name = f[:len(f)-5]
		if d.Timeout == 0 {
			d.Timeout = ConfigV.GetInt("devicedefault.timeout")
		}
		if d.Every == 0 {
			d.Every = ConfigV.GetInt("devicedefault.every")
		}
		if d.Rotate == 0 {
			d.Rotate = ConfigV.GetInt("devicedefault.rotate")
		}
		if d.Command == "" {
			d.Command = ConfigV.GetString("devicedefault.command")
		}
		if d.PortSSH == 0 {
			p, err := strconv.Atoi(ConfigV.GetString("devicedefault.portshh"))
			if err != nil {
				d.PortSSH = 22
			}

			if err == nil || p < 655535 || p > 0 {
				d.PortSSH = uint16(p)
			} else {
				d.PortSSH = 22
			}

		}
		d.Authkey = false
		if d.Key != "" {
			d.Authkey = true
		}

		if d.Authkey == false && d.Password == "" && d.Key != "" {
			d.Authkey = true
		}
		if d.Authkey == false && d.Password == "" && d.Key == "" && ConfigV.GetString("devicedefault.key") != "" {
			d.Authkey = true
			d.Key = ConfigV.GetString("devicedefault.key")
		}
		d.Dirbackup = fmt.Sprintf("%s/%s", ConfigV.GetString("path.backup"), d.Name)
		MDeviceList[d.Name] = d
		Dev.Devices = append(Dev.Devices, d)
	}
}

func RotateDevice(Dev *DeviceList) {
	LogConsole.Info("Rotate device list...")
	for i, d := range Dev.Devices {

		if _, err := os.Stat(d.Dirbackup); os.IsNotExist(err) {
			_ = os.Mkdir(d.Dirbackup, os.ModePerm)
			LogConsole.Info(fmt.Sprintf("Create Folder %s for backup ", d.Dirbackup))
			continue
		}
		files, err := ioutil.ReadDir(d.Dirbackup)
		if err != nil {
			es := fmt.Sprintf("Error read folder backup %s, Error:%s", d.Dirbackup, err.Error())
			LogConsole.Error(es)

			Dev.Devices[i] = Device{}

			continue
		}

		for _, f := range files {
			re := regexp.MustCompile(`\.log$`)
			if reg := re.FindString(f.Name()); reg != "" {
				continue
			}
			now := time.Now()
			fdifftimesecond := now.Sub(f.ModTime()).Seconds()
			diffday := fdifftimesecond / 60 / 24
			if int(diffday) > d.Rotate {
				if len(files) > 5 {
					err := os.Remove(f.Name())
					if err != nil {
						es := fmt.Sprintf("Error Remove file %s, Error:%s", f.Name(), err.Error())
						LogConsole.Error(es)
						continue
					}
				}

			}
			if int(fdifftimesecond) < d.Every {

				Dev.Devices[i] = Device{}

				break
			}

		}

	}

	newDev := DeviceList{}
	for _, d := range Dev.Devices {
		if d.Name == "" {
			continue
		}
		newDev.Devices = append(newDev.Devices, d)
	}
	Dev.Devices = newDev.Devices

}

func RunBackups(Dev *DeviceList) {
	LogConsole.Info("Backup Start ---->")
	wp := workerpool.New(ConfigV.GetInt("process.max"))
	for _, d := range Dev.Devices {
		d := d
		var LogDevice = logrus.New()
		wp.Submit(func() {
			backup(d, LogDevice)
		})

	}
	wp.StopWait()
	LogConsole.Info("Backup Finish <----")
}

func backup(d Device, LogDevice *logrus.Logger) {

	LogConsole.Warn(fmt.Sprintf("Starting backup %s ...", d.Name))

	switch ConfigV.GetString("log.format") {
	case "text":
		LogDevice.SetFormatter(&logrus.TextFormatter{})
	case "json":
		LogDevice.SetFormatter(&logrus.JSONFormatter{})
	default:
		LogDevice.SetFormatter(&logrus.TextFormatter{})
	}
	LogDevice.SetOutput(&lumberjack.Logger{
		Filename:   fmt.Sprintf("%s/%s", d.Dirbackup, "device.log"),
		MaxSize:    0,
		MaxAge:     ConfigV.GetInt("log.maxday"),
		MaxBackups: 0,
		LocalTime:  true,
		Compress:   false,
	})
	var bytefromsshclient []byte
	if d.Parent != "" {
		parent, child, err := SshNeedForward(d)
		if err != nil {
			ers := fmt.Sprintf("Fatal error Device:%s get parent %s error: %s", d.Name, d.Parent, err.Error())
			LogConsole.Error(ers)
			LogDevice.Error(ers)
			return
		}
		newd := SshForwardNewDevice(parent, child)

		bytefromsshclient, err = SshClientRun(newd)
		if err != nil {
			ers := fmt.Sprintf("Fatal error Forward Device:%s: %s", d.Name, err.Error())
			LogConsole.Error(ers)
			LogDevice.Error(ers)
			return
		}
	} else {
		bytefromssh, err := SshClientRun(d)
		if err != nil {
			ers := fmt.Sprintf("Fatal error Device:%s: %s", d.Name, err.Error())
			LogConsole.Error(ers)
			LogDevice.Error(ers)
			return
		}
		bytefromsshclient = bytefromssh
	}

	if bytefromsshclient == nil {
		ers := fmt.Sprintf("Fatal error Device:%s not bytes for backup", d.Name)
		LogConsole.Error(ers)
		LogDevice.Error(ers)
		return
	}
	dt := time.Now().Format("20060102T1504")
	if d.NameBackupPrefix != "" {
		dt = fmt.Sprintf("%s%s", d.NameBackupPrefix, dt)
	}

	backupfile := fmt.Sprintf("%s/%s.rsc", d.Dirbackup, dt)
	LogDevice.Info(fmt.Sprintf("Create file backup %s...", backupfile))
	fn, err := os.Create(backupfile)
	if err != nil {

		ers := fmt.Sprintf("Fatal error Device:%s create file %s, Error:%s", d.Name, backupfile, err.Error())
		LogConsole.Error(ers)
		LogDevice.Error(ers)
		return
	}
	LogDevice.Info(fmt.Sprintf("Write to file backup %s ...", backupfile))
	_, err = fn.Write(bytefromsshclient)
	if err != nil {

		ers := fmt.Sprintf("Fatal error Device:%s write to file %s, Error:%s", d.Name, backupfile, err.Error())
		LogConsole.Error(ers)
		LogDevice.Error(ers)
		return
	}

	_ = fn.Close()
	LogDevice.Info("Backup complete")

}
