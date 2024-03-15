import os
import subprocess
import sys
import time
import dotenv
import jinja2
import datetime
import argparse

import requests

parser = argparse.ArgumentParser(description='Process cicd parameters')

parser.add_argument('--registry-image',
                   help='registry image.', default="f5devcentral/k8s-bigip-ctlr-c")
parser.add_argument('--registry-user',
                   help='registry user.', required=True)
parser.add_argument('--registry-password',
                   help='registry password.', required=True)
parser.add_argument('--ci-commit-tag',
                   help='commit tag.', default="test")
args = parser.parse_args()
print(args)

dts = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")

curdir = os.path.dirname(os.path.abspath(sys.argv[0]))
deploy_yaml = os.path.join('/tmp', 'deploy-f5-cis-pod-%s.yaml' % dts)
mark_file = "/tmp/test_pipeline_is_running"
env_file = '/root/env'
log_file = ' /tmp/k8s-bigip-ctlr-c-%s.log' % dts

def source_env():
    dotenv.load_dotenv(env_file)
    if not 'BIGIP_URL' in os.environ or not 'BIGIP_USERNAME' in os.environ or not 'BIGIP_PASSWORD' in os.environ:
        raise Exception("missing BIGIP_* environments in %s" % env_file)

class StartTest:
    def __init__(self):
        pass
    def __enter__(self):
        if os.access(mark_file, os.F_OK):
            with open(mark_file) as fr:
                test_info = fr.read()
                raise Exception("another test is processing: %s" % test_info)
        else:
            with open(mark_file, 'w') as fw:
                fw.write("registry: %s:latest-%s\n" % (args.registry_image, dts))
    def __exit__(self, exc_type, exc_val, exc_tb):
        os.remove(mark_file)

def clean_k8s_bigip():
    kic_debug_url = 'http://localhost:30082'
    resp = requests.get(kic_debug_url)
    resp.raise_for_status()
    print("accessing %s response: %d" % (kic_debug_url, resp.status_code))
    jresp = resp.json()
    for k, v in jresp.get("cfgs", {}).items():
        if 'res' in v and 'Cmkey' in v['res']:
            nsn = v['res']['Cmkey']
            print("removing %s" % nsn)
            ns = nsn.split('/')[0]
            n = nsn.split('/')[1]
            kube_delete = subprocess.run('kubectl delete cm %s -n %s' % (n, ns), shell=True)
            kube_delete.check_returncode()

with StartTest() as st:

    print("cleaning environment for test...")
    clean_k8s_bigip()

    print("generating deploy-f5-cis-pod.yaml file...")
    with open(os.path.join(curdir, 'deploy-f5-cis-pod.yaml.j2')) as fr:
        tmpl = jinja2.Template(fr.read())
        rndr = tmpl.render(**{'docker_image': '%s:latest-%s' % (args.registry_image, dts)})
        print(rndr)
        with open(deploy_yaml, 'w') as fw:
            fw.write(rndr)

    print("logging in docker account %s..." % args.registry_user)
    docker_login = subprocess.run("docker login -u %s -p %s" % (args.registry_user, args.registry_password), shell=True)
    docker_login.check_returncode()

    print("building the image latest-%s..." % dts)
    make_build = subprocess.run("make -C %s -e version=latest -e timestamp=%s -e docker_repo=%s" % (os.path.join(curdir, "../build"), dts, args.registry_image), shell=True)
    make_build.check_returncode()

    print("deploying cis-c...")
    deploy_cisc = subprocess.run("kubectl apply -f %s" % deploy_yaml, shell=True)
    deploy_cisc.check_returncode()
    time.sleep(10)

    print("verifying cis-c deployment...")
    check_deploy = subprocess.run("kubectl get deploy k8s-bigip-ctlr-c -n kube-system -o json | jq .status", shell=True)
    check_deploy.check_returncode()

    print("running the test...")
    run_test = subprocess.run("python3 %s --config %s" % (os.path.join(curdir, "../tests/funcs/test.automation/test.py"), env_file), shell=True)
    run_test.check_returncode()

    print("collecting cis-c logs...")
    gather_testlog = subprocess.run("kubectl logs deployment/k8s-bigip-ctlr-c -c k8s-bigip-ctlr-c-pod -n kube-system > %s" % log_file, shell=True)
    gather_testlog.check_returncode()

    if args.ci_commit_tag != "test":
        print("pushing docker image: f5devcentral/k8s-bigip-ctlr-c:%s..." % args.ci_commit_tag)
        tag_image = subprocess.run("docker tag %s:latest-%s f5devcentral/k8s-bigip-ctlr-c:%s" % (args.registry_image, dts, args.ci_commit_tag), shell=True)
        tag_image.check_returncode()
        push_image = subprocess.run("docker push f5devcentral/k8s-bigip-ctlr-c:%s" % args.ci_commit_tag, shell=True)
        push_image.check_returncode()

    print('************ Collected Logs : %s ************' % log_file)
    print('************ Generated Image: %s:latest-%s ************' % (args.registry_image, dts))
    print('************ Deployment Yaml: %s ************' % deploy_yaml)