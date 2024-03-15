# generate deployment yaml
import base64
import getopt
import subprocess
import json
import os
import sys
import time
import yaml
import jinja2
import requests
import glob
import urllib3
from urllib3.exceptions import InsecureRequestWarning
urllib3.disable_warnings(InsecureRequestWarning)

homedir = os.path.abspath(os.path.dirname(sys.argv[0]))
envdir = "%s/environment" % homedir
casesdir = "%s/testcases" % homedir
tmpldir = "%s/templates" % homedir
k8svers = "v0.0"

TIMEOUT = 15
DELAY   = 1

def load_env_vars(env_file):
    with open(env_file) as fr:
        while True:
            line = fr.readline().strip()
            if line:
                # print(line)
                k, v = line.split("=")
                os.environ[k] = v
            else:
                break
    # for k, v in os.environ.items():
    #     print("%20s: %s" % (k, v))

def collect_test_cases(case_files=None):
    testcases = {}
    if case_files != None:
        for cf in case_files:
            with open(cf) as fr:
                testcases[cf] = yaml.safe_load(fr)
                if testcases[cf] == None:
                        testcases[cf] = []
                else:
                    filtered = []
                    for t in testcases[cf]:
                        if t.get("k8s_version", None) != None and t['k8s_version'] != k8svers:
                            pass
                        else:
                            filtered.append(t)
                    testcases[cf] = filtered
    else:
        dir_files = glob.glob("%s/**/*" % casesdir, recursive=True)
        for fp in dir_files:
            if os.path.isfile(fp):
                with open(os.path.join(casesdir, fp)) as fr:
                    testcases[fp] = yaml.safe_load(fr)
                    if testcases[fp] == None:
                        testcases[fp] = []
                    else:
                        filtered = []
                        for t in testcases[fp]:
                            if t.get("k8s_version", None) != None and t['k8s_version'] != k8svers:
                                pass
                            else:
                                filtered.append(t)
                        testcases[fp] = filtered
    return testcases

def gen_kube_yaml(case_file, case):
    # print(case)
    configmap_name = os.path.basename(case_file).replace(".", "-")
    name = case["name"].replace(" ", "-").replace("_", "-")
    j2tmpl_path = os.path.join(tmpldir, case["jinja2"])
    as3tmpl = case.get('template', {})
    as3tmplstr = as3tmpl if type(as3tmpl) == str else json.dumps(as3tmpl)
    tmpl = jinja2.Template(as3tmplstr)
    vars = case.get('variables', {})
    rndr = tmpl.render(**vars)
    rndrjson = json.loads(rndr)
    
    with open(j2tmpl_path) as fr:
        content = fr.read()
        template = jinja2.Template(content)
        rendered = template.render(
            configmap_name="a"+configmap_name,
            case_template=json.dumps(rndrjson),
            **vars
    )
    target = "%s/tmp-%s-%s.yaml" % ('/var/tmp', os.path.basename(case_file), name)
    with open(target, "w") as fw:
        fw.write(rendered)
    return target

def apply_kube_yaml(name, yaml_file):
    cmd = "kubectl --kubeconfig %s apply -f %s" % (os.environ['KUBE_CONFIG_FILEPATH'], yaml_file)
    warn(name, "Deploying ... %s" % cmd)
    cp = subprocess.run(cmd, shell=True, stderr=subprocess.PIPE, stdout=subprocess.PIPE)
    # print(cp)
    if cp.returncode != 0:
        fail(name, "Failed to deploy: %s" % str(cp.stderr, 'utf-8'))
    else:
        ok(name, "%s" % str(cp.stdout, 'utf-8'))

def delete_kube_yaml(name, yaml_file):
    cmd = "kubectl --kubeconfig %s delete -f %s" % (os.environ['KUBE_CONFIG_FILEPATH'], yaml_file)
    warn(name, "Deleting ... %s" % cmd)
    cp = subprocess.run(cmd, shell=True, stderr=subprocess.PIPE, stdout=subprocess.PIPE)
    # print(cp)
    if cp.returncode != 0:
        fail(name, "Failed to delete: %s" % str(cp.stderr, 'utf-8'))
    else:
        ok(name, "%s" % str(cp.stdout, 'utf-8'))

def setup_case(name, setups):
    for setup in setups:
        warn(name, "Setup %s" % setup)
        cp = subprocess.run(setup, shell=True, stderr=subprocess.PIPE, stdout=subprocess.PIPE)
        # print(cp)
        if cp.returncode != 0:
            fail(name, "Failed to setup %s: %s" % (setup, str(cp.stderr, 'utf-8')))
        else:
            ok(name, "%s" % str(cp.stdout, 'utf-8'))

def teardown_case(name, teardowns):
    for td in teardowns:
        warn(name, "Teardown %s" % td)
        cp = subprocess.run(td, shell=True, stderr=subprocess.PIPE, stdout=subprocess.PIPE)
        # print(cp)
        if cp.returncode != 0:
            fail(name, "Failed to teardown %s: %s" % (td, str(cp.stderr, 'utf-8')))
        else:
            ok(name, "%s" % str(cp.stdout, 'utf-8'))

def fail(name, msg):
    lines = msg.split("\n")
    for line in lines:
        print("  \033[1;35mFailed\033[0m : %-30s %s" % (name, line))
    raise Exception("Details")

def ok(name, msg=""):
    lines = msg.split("\n")
    for line in lines:
        print("  \033[1;32mOK\033[0m     : %-30s %s" % (name, line))

def note(name, msg=""):
    lines = msg.split("\n")
    for line in lines:
        print("  ->     : %-30s %s" % (name, line))

def warn(name, msg):
    lines = msg.split("\n")
    for line in lines:
        print("  \033[1;30m...\033[0m    : %-30s %s" % (name, line))

def assert_resources_operated(name, checks):
    headers = {
        'content-type': 'application/json',
        'Authorization': 'Basic %s' % 
            base64.b64encode(('%s:%s'%(os.environ["BIGIP_USERNAME"],os.environ["BIGIP_PASSWORD"])).encode()).decode()
    }
    for check in checks:
        base_url = '%s%s' % (os.environ["BIGIP_URL"], check["uri"])
        timeout = TIMEOUT
        if check.get("timeout", None) != None:
            timeout = check['timeout']
        while timeout > 0:
            timeout -= 1
            warn(name, "Checking %s (retrying time: %d)" % (base_url, timeout))
            resp = None
            try:
                resp = requests.get(base_url, verify=False, headers=headers)
            except Exception as e:
                warn(name, "Exception %s (retrying time: %d)" % (e, timeout))
                time.sleep(DELAY)
                continue

            # check status
            if resp.status_code != check["status"]:
                if timeout == 0:
                    fail(name, "%d %s" % (resp.status_code, resp.text))
                else:
                    time.sleep(DELAY)
                    continue
            else:
                pass
            
            # check obj
            objs = resp.json()
            if check.get("body", None) != None:
                rlt, prop, expected, actual = check_body_readiness(name, check['body'], objs)
                if not rlt:
                    if timeout == 0:
                        ok(name, "Response: %s" % json.dumps(objs, indent=2))
                        fail(name, "Field '%s' Expect: %s, Actually: %s" % (prop, expected, actual))
                    else:
                        time.sleep(DELAY)
                        continue
                else:
                    break
            else:
                break
                   
        ok(name, "Checked %s" % check)

def check_body_readiness(name, expected, actual):
    if type(actual)!= type(expected):
        return (False, name, type(expected), type(actual))

    if type(expected) == str or type(expected) == int or type(expected) == bool:
        if actual != expected:
            return (False, name, expected, actual)
    elif type(expected) == list:
        if len(actual) != len(expected):
            return (False, name, "length %d" % len(expected), "length %d" % len(actual))
        for n in range(0, len(expected)):
            rlt, p, e, a = check_body_readiness(name, expected[n], actual[n])
            if not rlt:
                return (rlt, p, e, a)
    elif type(expected) == dict:
        for prop in expected.keys():
            if actual.get(prop, None) == None:
                return(False, name, prop, "key not exist")
            rlt, p, e, a = check_body_readiness(prop, expected[prop], actual[prop])
            if not rlt:
                return (rlt, p, e, a)
    else:
        fail(name, "More coding work needed: type %s" % expected[prop])

    return (True, "", "", "")


def get_kube_version():
    cmd = "kubectl --kubeconfig %s version -o json" % (os.environ['KUBE_CONFIG_FILEPATH'])
    cp = subprocess.run(cmd, shell=True, stderr=subprocess.PIPE, stdout=subprocess.PIPE)
    # print(cp)
    if cp.returncode != 0:
        fail("k8s version", "Failed to deploy: %s" % str(cp.stderr, 'utf-8'))
    else:
        ok("k8s version", "%s" % str(cp.stdout, 'utf-8'))

    verjson = json.loads(cp.stdout)
    return "v%s.%s" % (verjson["serverVersion"]["major"], verjson["serverVersion"]["minor"])

opts, args = getopt.getopt(sys.argv[1:], '', ['help', 'config='])
env_file = ""
for opt, arg in opts:
    if opt == "--help":
        print("%s --config=/path/to/env_file [casefiles|..]" % sys.argv[0])
        sys.exit(0)
    if opt == "--config":
        env_file = arg

if env_file == "":
    warn("ERROR", "%s --config=/path/to/env_file [casefiles|..]" % sys.argv[0])
    sys.exit(1)
load_env_vars(env_file)

k8svers = get_kube_version()

case_files = args if len(args) > 0 else None
testcases = collect_test_cases(case_files)
flcount = len(testcases.keys())
total = 0
for case_file in testcases.keys():
    total += len(testcases[case_file])

index = 0
for case_file in sorted(testcases.keys()):
    testcases_in_file = testcases[case_file]
    try:
        count = len(testcases_in_file)
        if count == 0:
            continue
        for i in range(0, count):
            index += 1
            case = testcases_in_file[i]
            name = "(%d/%d) %s" % (index, total, case["name"])
            note(name, "Testing %s" % case_file)
            kube_yaml = gen_kube_yaml(case_file, case)
            if case.get("setup", None) != None:
                setup_case(name, case['setup'])
            apply_kube_yaml(name, kube_yaml)
            assert_resources_operated(name, case["checks"]["apply"])
        delete_kube_yaml(name, kube_yaml)
        assert_resources_operated(name, case["checks"]["delete"])
        if case.get('teardown', None) != None:
            teardown_case(name, case['teardown'])
    except Exception as e:
        warn("ERROR", str(e))
        warn("ERROR", "yaml file: %s" % kube_yaml)
        sys.exit(1)

