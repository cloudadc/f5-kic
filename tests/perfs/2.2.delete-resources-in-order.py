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


if len(sys.argv) != 3:
    print("<start> <end>")
    sys.exit(1)

index = int(sys.argv[1])
count = int(sys.argv[2])

with open('0.7.test-vars.json') as fr:
    jd = json.load(fr)
    bigip_ipaddr = jd['bigip_ipaddr']
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
            '-e', 'action=delete'],
        project_dir=homedir
    )

    if rc != 0:
        raise Exception("failed in running playbook for index %d: %d, %s" % (
            index, rc, err))
    
    return datetime.datetime.now()

def rest_deleted_on_bigip(index):
    base_url = 'https://%s:8443/mgmt/tm' % bigip_ipaddr
    headers = {
        'content-type': 'application/json',
        'Authorization': 'Basic %s' % base64.b64encode('admin:P@ssw0rd123'.encode()).decode()
    }
    try:
        path = 'kubernetes'
        prefix = 'default_%s' % resource_prefix

        resp = requests.get('%s/ltm/virtual/~%s~%ss%d' % (base_url, path, prefix, index),
                            verify=False, headers=headers)
        vs_nexists = resp.status_code == 404

        resp = requests.get('%s/ltm/pool/~%s~%ss%d' % (base_url, path, prefix, index),
                            verify=False, headers=headers)
        pl_nexists = resp.status_code == 404

    except Exception as e:
        print(e)
        return False

    return vs_nexists and pl_nexists


while index < count:
    try:
        done_dt = run_playbook_for_resource(index)
        while True:
            print('waiting for resource %d to be deleted on bigip' % index)
            nexists = rest_deleted_on_bigip(index)
            
            if not nexists:
                time.sleep(3)
                continue

            break

        index += 1
    except Exception as e:
        raise Exception("failed with reason: %s" % e)
