// Package bean 对应 net.rwhps.server.data.bean 下的配置类
package bean

import (
	"reflect"
	"strconv"
	"strings"

	"rwhps/internal/util/algorithms"
	"rwhps/internal/util/file"
	"rwhps/internal/util/jsonutil"
	"rwhps/internal/util/log"
	"rwhps/internal/util/systemprops"
)

// jsonName 返回字段的 json 名称 (无 tag 时用字段名)
func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	name := strings.Split(tag, ",")[0]
	if name == "" || name == "-" {
		return f.Name
	}
	return name
}

// CoverField 对应 AbstractBeanConfig.coverField
func CoverField(bean any, name string, value any) bool {
	v := reflect.ValueOf(bean)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return false
	}
	v = v.Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if jsonName(f) != name {
			continue
		}
		fv := v.Field(i)
		if !fv.CanSet() {
			return false
		}
		str := toString(value)
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(str)
		case reflect.Bool:
			fv.SetBool(strings.EqualFold(str, "true"))
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if n, err := strconv.ParseInt(str, 10, 64); err == nil {
				fv.SetInt(n)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if n, err := strconv.ParseUint(str, 10, 64); err == nil {
				fv.SetUint(n)
			}
		case reflect.Float32, reflect.Float64:
			if n, err := strconv.ParseFloat(str, 64); err == nil {
				fv.SetFloat(n)
			}
		}
		return true
	}
	return false
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return reflect.ValueOf(v).String()
}

// ReadProperty 对应 AbstractBeanConfig.readProperty
func ReadProperty(bean any, prefix string, f *file.FileUtils) {
	if prefix == "" {
		log.Trackf("Config Property %s no Property", f.Name())
		return
	}
	v := reflect.ValueOf(bean).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name := jsonName(t.Field(i))
		key := prefix + "." + name
		data, ok := systemprops.Get(key)
		if !ok {
			continue
		}
		if algorithms.Base64IsBase64(data) {
			if decoded, err := algorithms.Base64DecodeString(data); err == nil {
				data = decoded
			}
		}
		if CoverField(bean, name, data) {
			log.Debugf("Set OK %s = %s", name, data)
		} else {
			log.Debugf("Set ERROR %s = %s", name, data)
		}
	}
}

// Save 对应 AbstractBeanConfig.save
func Save(bean any, f *file.FileUtils) {
	f.WriteFile(jsonutil.ToPrettyJSON(bean))
}

// LoadOrInit 从文件加载配置, 文件不存在或为空时使用默认值并落盘
func LoadOrInit[T any](f *file.FileUtils, defaults T) T {
	data := f.ReadFileStringData()
	if strings.TrimSpace(data) == "" {
		Save(defaults, f)
		return defaults
	}
	out := defaults
	if err := jsonutil.FromJSON(data, &out); err != nil {
		log.Errorf("Config parse error %s: %v", f.Name(), err)
		Save(defaults, f)
		return defaults
	}
	return out
}
