package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/donnie4w/dom4g"
	_ "github.com/donnie4w/dom4g"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	SrcPath string
	DetPath string
}

var (
	config    Config
	ch        = make(chan string)
	fileCount int
)

func main() {
	log.SetFormatter(&log.TextFormatter{ForceColors: true, FullTimestamp: true})
	startTime := time.Now().UnixNano()
	c := flag.String("C", "./config.json", "配置文件路径")
	flag.Parse()

	config = Config{}
	//读取json配置
	data, err := ioutil.ReadFile(*c)
	checkError(err)

	err = json.Unmarshal(data, &config)
	checkError(err)

	err = createDir(config.DetPath)
	checkError(err)
	err = createDir(config.DetPath + "mapbase")
	checkError(err)
	err = createDir(config.DetPath + "mapPlist")
	checkError(err)

	//删除文件夹特定后缀文件
	filepath.Walk(config.DetPath, walkFunc)

	//遍历打印所有的文件名
	filepath.Walk(config.SrcPath, walkFunc2)
	count := 0
	for {
		_, open := <-ch
		if !open {
			break
		}
		count++
		if count == fileCount {
			break
		}
	}

	endTime := time.Now().UnixNano()
	log.Infof("总耗时:%v毫秒\n", (endTime-startTime)/1000000)
	time.Sleep(time.Millisecond * 1500)
}

func walkFunc(files string, info os.FileInfo, err error) error {
	os.Remove(files)
	return nil
}

func walkFunc2(files string, info os.FileInfo, err error) error {
	_, fileName := filepath.Split(files)
	// fmt.Println(paths, fileName)      //获取路径中的目录及文件名
	// fmt.Println(filepath.Base(files)) //获取路径中的文件名
	// fmt.Println(path.Ext(files))      //获取路径中的文件的后缀
	if path.Ext(files) == ".tmx" && !strings.HasPrefix(fileName, "~$") {
		fileCount++
		go readXml(files, strings.Replace(fileName, ".tmx", "", -1))
	}
	return nil
}

func readXml(path string, fileName string) {
	content, err := ioutil.ReadFile(path)
	checkError(err)

	el, err := dom4g.LoadByXml(string(content))
	checkError(err)

	_map := el.GetNodeByPath("map")

	output := make(map[string]interface{}, 1)
	output["width"] = decimal(getFloat(_map, "width") * getFloat(_map, "tilewidth"))
	output["height"] = decimal(getFloat(_map, "height") * getFloat(_map, "tileheight"))
	output["name"] = fileName + ".tmx"

	groups := el.Nodes("objectgroup")

	for i := 0; i < len(groups); i++ {
		groupName, _ := groups[i].AttrValue("name")
		groupsOutput := make([]map[string]interface{}, 0)
		objs := groups[i].Nodes("object")
		for j := 0; j < len(objs); j++ {
			obj := objs[j]

			ret := make(map[string]interface{}, 1)
			ret["w"] = decimal(getFloat(obj, "width"))
			ret["h"] = decimal(getFloat(obj, "height"))
			x := -output["width"].(float64)/2.0 + ret["w"].(float64)/2.0 + getFloat(obj, "x")
			y := output["height"].(float64)/2.0 - ret["h"].(float64)/2.0 - getFloat(obj, "y")
			var b bool
			_, b = obj.AttrValue("type")
			if b {
				ret["type"] = getString(obj, "type")
			}

			ret["objId"] = getInt(obj, "id")

			_, b = obj.AttrValue("name")
			if b {
				ret["name"] = getString(obj, "name")
			}

			_, b = obj.AttrValue("rotation")
			if b {
				ret["rotation"] = decimal(getFloat(obj, "rotation"))

				w := ret["w"].(float64)
				h := ret["h"].(float64)

				rot1 := ret["rotation"].(float64) * math.Pi / 180
				rot2 := math.Atan2(h, w)
				allRot := rot1 + rot2
				len := math.Sqrt(math.Pow(w*0.5, 2) + math.Pow(h*0.5, 2))
				x = x + len*math.Cos(allRot) - w*0.5
				y = y + h - len*math.Sin(allRot) - h*0.5
			}
			ret["x"] = decimal(x)
			ret["y"] = decimal(y)

			getPolygon(obj, ret)
			parseProperty(obj, ret)
			groupsOutput = append(groupsOutput, ret)
		}
		output[groupName] = groupsOutput
	}

	imagelayers := el.Nodes("imagelayer")
	var imagelayersOutput []map[string]interface{}
	for i := 0; i < len(imagelayers); i++ {
		imagelayer := imagelayers[i]
		image := imagelayer.Nodes("image")

		ret := make(map[string]interface{}, 0)
		ret["source"] = getString(image[0], "source")

		ret["w"] = decimal(getFloat(image[0], "width"))
		ret["h"] = decimal(getFloat(image[0], "height"))
		ret["x"] = int64(-output["width"].(float64)/2.0 + ret["w"].(float64)/2.0 + getFloat(imagelayer, "offsetx"))
		ret["y"] = int64(output["height"].(float64)/2.0 - ret["h"].(float64)/2.0 - getFloat(imagelayer, "offsety"))
		ret["name"] = getString(imagelayer, "name")

		parseProperty(imagelayer, ret)
		imagelayersOutput = append(imagelayersOutput, ret)
	}
	output["imagelayers"] = imagelayersOutput

	tilesets := el.Nodes("tileset")
	var tilesetsOutput []map[string]interface{}
	for i := 0; i < len(tilesets); i++ {
		tileset := tilesets[i]
		image := tileset.Nodes("image")

		ret := make(map[string]interface{}, 0)

		var source = getString(image[0], "source")
		ret["source"] = source
		ret["w"] = decimal(getFloat(image[0], "width"))
		ret["h"] = decimal(getFloat(image[0], "height"))
		var strList = strings.Split(source, "/")
		ret["name"] = strings.Split(strList[len(strList)-1], ".")[0]
		tilesetsOutput = append(tilesetsOutput, ret)
	}
	output["tilesets"] = tilesetsOutput

	writeJSON(config.DetPath, fileName, output)

	//读取PLIST 文件
	bytes, err := ioutil.ReadFile(config.DetPath + "mapPlist/" + fileName + ".plist")
	plist := string(bytes)
	for k := 0; k < len(imagelayersOutput); k++ {
		if err != nil {
			imgFile := imagelayersOutput[k]["source"].(string)
			createDir(filepath.Dir(config.SrcPath + imgFile))
			createDir(filepath.Dir(config.DetPath + imgFile))
			bytes, _ := ioutil.ReadFile(config.SrcPath + imgFile)
			err := ioutil.WriteFile(config.DetPath+imgFile, bytes, 0666)
			checkError(err)
		} else {
			imgFile := imagelayersOutput[k]["source"].(string)
			if strings.Index(plist, string([]rune(imgFile)[8:])) == -1 {
				createDir(filepath.Dir(config.SrcPath + imgFile))
				createDir(filepath.Dir(config.DetPath + imgFile))

				bytes, _ := ioutil.ReadFile(config.SrcPath + imgFile)
				err := ioutil.WriteFile(config.DetPath+imgFile, bytes, 0666)
				checkError(err)
			}
		}
	}

	for k := 0; k < len(tilesetsOutput); k++ {
		if err != nil {
			imgFile := tilesetsOutput[k]["source"].(string)
			createDir(filepath.Dir(config.SrcPath + imgFile))
			createDir(filepath.Dir(config.DetPath + imgFile))
			bytes, _ := ioutil.ReadFile(config.SrcPath + imgFile)
			err := ioutil.WriteFile(config.DetPath+imgFile, bytes, 0666)
			checkError(err)
		} else {
			imgFile := tilesetsOutput[k]["source"].(string)
			if strings.Index(plist, string([]rune(imgFile)[8:])) == -1 {
				createDir(filepath.Dir(config.SrcPath + imgFile))
				createDir(filepath.Dir(config.DetPath + imgFile))

				bytes, _ := ioutil.ReadFile(config.SrcPath + imgFile)
				err := ioutil.WriteFile(config.DetPath+imgFile, bytes, 0666)
				checkError(err)
			}
		}
	}

	ch <- fileName
}

//创建文件夹
func createDir(dir string) error {
	exist, err := pathExists(dir)
	checkError(err)

	if exist {

	} else {
		//创建文件夹
		err := os.MkdirAll(dir, os.ModePerm)
		checkError(err)
	}
	return nil
}

//判断文件夹是否存在
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func getInt(el *dom4g.Element, name string) int64 {
	val, b := el.AttrValue(name)
	if !b {
		return 0
	}
	return toInt(val)
}

func toInt(s string) int64 {
	f, err := strconv.ParseFloat(s, 32)
	checkError(err)
	return int64(f)
}

func getFloat(el *dom4g.Element, name string) float64 {
	val, b := el.AttrValue(name)
	if !b {
		return 0
	}
	return toFloat(val)
}

func toFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 32)
	checkError(err)
	return f
}

func decimal(value float64) float64 {
	value, _ = strconv.ParseFloat(fmt.Sprintf("%.2f", value), 64)
	return value
}

func getString(el *dom4g.Element, name string) string {
	val, b := el.AttrValue(name)
	if !b {
		return ""
	}
	return val
}

func parseProperty(el *dom4g.Element, ret map[string]interface{}) {
	properties := el.Node("properties")
	if properties == nil {
		return
	}
	propertys := properties.Nodes("property")
	for k := 0; k < len(propertys); k++ {
		property := propertys[k]
		saveProperty(property, ret)
	}
}

func saveProperty(property *dom4g.Element, ret map[string]interface{}) {
	key, _ := property.AttrValue("name")
	t, _ := property.AttrValue("type")
	val, _ := property.AttrValue("value")

	var value interface{}
	if "bool" == t {
		if val == "false" {
			value = false
		} else {
			value = true
		}
	} else if "int" == t {
		value = toInt(val)
	} else if "float" == t {
		value = decimal(toFloat(val))
	} else {
		value = val
	}

	ret[key] = value
}

//写JSON文件
func writeJSON(path string, fileName string, dataDict map[string]interface{}) {
	file, err := os.OpenFile(path+"/"+fileName+".json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666) //不存在创建清空内容覆写
	checkError(err)

	defer file.Close()
	//字典转字符串
	file.WriteString(map2Str(dataDict))
}

//字典转字符串
func map2Str(dataDict map[string]interface{}) string {
	b, err := json.Marshal(dataDict)
	if err != nil {
		log.Errorln(err)
		return ""
	}
	return string(b)
}

func checkError(err error) {
	if err != nil {
		log.Fatalf("%v\n", err)
		return
	}
}

func getPolygon(el *dom4g.Element, ret map[string]interface{}) {
	var polygonArr []int64
	polygon := el.Nodes("polygon")

	if len(polygon) > 0 {
		points := strings.Split(getString(polygon[0], "points"), " ")
		p := strings.Split(points[0], ",")
		polygonArr = append(polygonArr, toInt(p[0]))
		polygonArr = append(polygonArr, -toInt(p[1]))
		for k := len(points) - 1; k >= 1; k-- {
			p := strings.Split(points[k], ",")
			polygonArr = append(polygonArr, toInt(p[0]))
			polygonArr = append(polygonArr, -toInt(p[1]))
		}
		ret["polygon"] = polygonArr
	}
}
