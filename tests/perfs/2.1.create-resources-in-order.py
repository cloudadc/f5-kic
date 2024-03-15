import base64
import sys
import os
import datetime
import time
import requests
import urllib3
from urllib3.exceptions import InsecureRequestWarning
urllib3.disable_warnings(InsecureRequestWarning)
import ansible_runner
import json


if len(sys.argv) != 4:
    print("<start> <end> <src>")
    print("src is the src file to be used in 2.0.crud-in-batch.yaml")
    print("e.g. 1.rest.resources.yaml.j2 or 1.ftp.resources.yaml.j2")
    print("e.g. python3 ./2.1.create-resources-in-order.py 2 4 1.rest.resources.yaml.j2")
    sys.exit(1)

index = int(sys.argv[1])
count = int(sys.argv[2])
src = sys.argv[3]

with open('0.7.test-vars.json') as fr:
    jd = json.load(fr)
    bigip_ipaddr = jd['bigip_ipaddr']
    bigip_password = jd['bigip_password']
    resource_prefix = jd['resource_prefix']

def run_playbook_for_resource(index):

    homedir = os.path.abspath(os.path.dirname(sys.argv[0]))
    create_yamls = [
        '2.0.crud-in-batch.yaml'
    ]
    out, err, rc = ansible_runner.run_command(
        executable_cmd='ansible-playbook',
        cmdline_args=create_yamls + [
            '-e', 'count=%d' % (index+1), 
            '-e', 'index=%d' % index, 
            '-e', 'src=%s' % src,
            '-e', 'action=apply'],
        project_dir=homedir
    )

    if rc != 0:
        raise Exception("failed in running playbook for index %d: %d, %s" % (
            index, rc, err))
    
    return datetime.datetime.now()

def rest_created_on_bigip(index):
    base_url = 'https://%s:8443/mgmt/tm' % bigip_ipaddr
    headers = {
        'content-type': 'application/json',
        'Authorization': 'Basic %s' % base64.b64encode(('admin:%s'%bigip_password).encode()).decode()
    }
    try:
        # TODO(x) need to modify and rename
        path = 'namespace%s' % index
        # prefix = 'default_%s' % resource_prefix
        prefix_app = 'service_name_app'
        prefix_vs = 'service_name_vs'

        resp = requests.get('%s/ltm/virtual/~%s~%s%d~%s%d' % (base_url, path, prefix_app, index, prefix_vs, index),
                            verify=False, headers=headers)
        vs_exists = resp.status_code == 200
        print("vs resp.status_code: %s" % resp.status_code)
        # print(resp.json())

        prefix_pool = 'service_name_pool'

        resp = requests.get('%s/ltm/pool/~%s~%s%d~%s%d' % (base_url, path, prefix_app, index, prefix_pool, index),
                            verify=False, headers=headers)
        pl_exists = resp.status_code == 200
        print("pool resp.status_code: %s" % resp.status_code)
        # print(resp.json())

        resp = requests.get('%s/ltm/pool/~%s~%s%d~%s%d/members' % (base_url, path, prefix_app, index, prefix_pool, index),
                            verify=False, headers=headers)
        print("members resp.status_code: %s" % resp.status_code)
        # print(resp.json())
    except Exception as e:
        print(e)
        return (False, [])

    mb_exists = False
    mbs = []
    if resp.status_code == 200:
        items = resp.json()['items']
        if len(items) > 0:
            mb_exists = True
            for n in items:
                mbs.append(n['address'])

    return ((vs_exists and pl_exists and mb_exists), mbs,)

def arp_created_on_bigip(name):
    base_url = 'https://%s:8443/mgmt/tm' % bigip_ipaddr
    headers = {
        'content-type': 'application/json',
        'Authorization': 'Basic %s' % base64.b64encode(('admin:%s'%bigip_password).encode()).decode()
    }

    try:
        an = "k8s"
        resp = requests.get('%s/net/arp/~Common~%s-%s' % (base_url, an, name),
                            verify=False, headers=headers)
    except Exception as e:
        print("failed to request: %s" % e)
        return False

    return resp.status_code == 200


while index < count:
    try:
        print('----------- creating resource %d --------' % index)
        done_dt = run_playbook_for_resource(index)
        ipaddrs = []
        while True:
            print('waiting for resource %d to be created on bigip' % index)
            exists, ipaddrs = rest_created_on_bigip(index)
            
            if not exists:
                time.sleep(1)
                continue
            else:
                print('----------- resource %d created now --------' % index)
            
            while True:
                print("waiting for arp %s to be created on bigip" % ipaddrs)
                exists = True
                for n in ipaddrs:
                    exists = arp_created_on_bigip(n)
                    if not exists: break
                if exists: break
                time.sleep(1)
            break

        index += 1
    except Exception as e:
        raise Exception("failed with reason: %s" % e)
