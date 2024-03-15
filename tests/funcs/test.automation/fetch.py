import getopt
import json
import requests
import os
import sys
import base64
import urllib3
from urllib3.exceptions import InsecureRequestWarning
urllib3.disable_warnings(InsecureRequestWarning)

opts, args = getopt.getopt(sys.argv[1:], "", ["config=", "help"])
env = ""
for opt, arg in opts:
    if opt == "--config":
        env = arg
    if opt == "--help":
        print("%s --config=/path/to/env_file [uri]" % sys.argv[0])

if env == "":
    print("config cannot be empty")
    sys.exit(1)

uri = args[0] if len(args) == 1 else None
if uri == None:
    print("no url appointed.")
    sys.exit(1)

curdir = os.path.dirname(sys.argv[0])

with open(env) as fr:
    lines = fr.readlines()
    for line in lines:
        kv = line.strip().split('=')
        os.environ[kv[0]] = kv[1]

base_url = "%s%s" % (os.environ["BIGIP_URL"], uri)
headers = {
    'content-type': 'application/json',
    'Authorization': 'Basic %s' % 
        base64.b64encode(('%s:%s'%(os.environ["BIGIP_USERNAME"],os.environ["BIGIP_PASSWORD"])).encode()).decode()
}

resp = requests.get(base_url, verify=False, headers=headers)
if resp.status_code == 200:
    print(json.dumps(resp.json(), indent=2))
else:
    print("%d %s" % (resp.status_code, resp.text))
