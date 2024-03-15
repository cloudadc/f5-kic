import json
import re
import yaml
import json
import glob

# 该文件用于读取当前目录下所有的tmp-*文件 生成AS3类型和AS3字段列表。
#
# tmp-*文件为业务下发配置yaml文件，内容为AS3格式的配置json字符串。
# 故，此脚本在执行完自动化测试脚本后执行，自动化测试脚本执行完后会生成各种临时文件。
# 其中就保存了测试中触及到的AS3类型和AS3字段列表。

kind_keys = {}

def sub_class(obj):
    if type(obj) == dict:
        if "class" in obj:
            if not obj["class"] in kind_keys:
                kind_keys[obj["class"]] = {}
            for k in obj.keys():
                kind_keys[obj["class"]][k] = ""
                sub_class(obj[k])


dir_files = glob.glob("./*.yaml", recursive=True)
# print(dir_files)
for fp in dir_files:
    with open(fp) as fr:
        yaml_content = yaml.safe_load_all(fr)
        for yc in yaml_content:
            if "data" in yc and "template" in yc["data"]:
                template = json.loads(yc["data"]["template"])
                # print(template)
                sub_class(template)

for k, v in kind_keys.items():
    if k == "AS3":
        kind_keys[k] = ['declaration']
    elif k == "ADC":
        kind_keys[k] = []
    elif k == "Application":
        kind_keys[k] = ['template']
    elif k == "Tenant":
        kind_keys[k] = []
    else:
        l = list(v.keys())
        l.remove("class")
        kind_keys[k] = l

# print(kind_keys)
# print(json.dumps(kind_keys, indent=2))
commons = ['AS3', 'ADC', 'Application', 'Tenant']
services = []
profiles = []
others = []
for k in kind_keys.keys():
    if k in commons:
        pass
    elif re.match(".*_Profile", k) != None:
        profiles.append(k)
    elif re.match("Service_.*", k) != None:
        services.append(k)
    else:
        others.append(k)
        
all = commons + services + profiles + others
for k in all:
    print("| %20s |%-70s|" % (k, kind_keys[k]))