
## 构建测试环境

测试环境有三个依赖：python3 ansible 及kubectl。三种依赖的解决方法如下：

* python 3.x
    ```shell
    $ export LANG=en_US.utf-8
    $ export LC_ALL=en_US.utf-8
    $ yum install -y python3
    ```

* 测试依赖包

    ```shell
    $ pip3 install --upgrade pip==21.1.0
    $ pip3 install ansible requests
    ```

* 安装kubectl

    ```shell
    $ wget http://10.250.11.185/downloads/kubectl -O /usr/local/bin/kubectl # 也可以从kubectl官网下载，国内访问时网速比较慢。
    $ chmod +x /usr/local/bin/kubectl
    ```

## 运行测试脚本

1. 环境变量定义

    ```shell
    # ====== /root/kube.config.yaml =======

    编辑：/root/kube.config.yaml文件，此文件来源于k8s相关配置，例如可以拷贝自 ~/.kube/config，当然，也可以直接使用 ~/.kube/config

    验证：kubectl --kubeconfig /root/kube.config.yaml get node

    # ====== /root/env =======

    编辑：/root/env文件，此文件是 test.py 脚本的配置文件，内容如下：

    BIGIP_URL=https://10.250.17.164:8443
    BIGIP_USERNAME=admin
    BIGIP_PASSWORD=P@ssw0rd123
    KUBE_CONFIG_FILEPATH=/root/kube.config.yaml

    请参考 tests/funcs/test.automation/environment/env.tmpl文件
    ```

    > 注意：这两个文件的命名和保存位置可以随意；下边的操作命令会使用到它们。

2. 下载源码


    可以从两个位置下载到源码：
    https://gitee.com/zongzw/f5-kic.git
    或者
    https://gitlab.f5net.com/cis-c/f5-kic

    后者是前者的镜像仓库；需要具备f5-kic repo的访问权限。

    ```shell
    $ git clone https://gitee.com/zongzw/f5-kic.git
    Cloning into 'f5-kic'...
    Username for 'https://gitee.com': zongzw
    Password for 'https://zongzw@gitee.com':
    remote: Enumerating objects: 2526, done.
    remote: Counting objects: 100% (2526/2526), done.
    remote: Compressing objects: 100% (2502/2502), done.
    remote: Total 2526 (delta 1661), reused 0 (delta 0), pack-reused 0
    Receiving objects: 100% (2526/2526), 706.62 KiB | 0 bytes/s, done.
    Resolving deltas: 100% (1661/1661), done.
    ```

3. 启动CIS-C

    按照正常方式启动，启动参数：
    ```yaml
    args: [
        "--bigip-username=$(BIGIP_USERNAME)",
        "--bigip-password=$(BIGIP_PASSWORD)",
        "--bigip-url=$(BIGIP_URL)",
        "--log-level=debug",
        "--flannel-name=fl-tunnel",
        "--hub-mode=true",  # ok as well: "--hub-mode"
        # "--namespace=default",
        "--ignore-service-port"
    ]
    ```

4. 执行测试脚本

    ```shell
    $ cd tests/funcs/test.automation
    $ python3 test.py --config /root/env
    ```

    执行结束后无`Failed`报错即为执行成功，例如：

    ```log
    OK     : (71/71) virtual ipv6 update
    ...    : (71/71) virtual ipv6 update    Checking https://10.250.18.105:8443/mgmt/tm/sys/folder/~test-virtual-tenant (retrying time: 14)
    ...    : (71/71) virtual ipv6 update    Checking https://10.250.18.105:8443/mgmt/tm/sys/folder/~test-virtual-tenant (retrying time: 13)
    OK     : (71/71) virtual ipv6 update    Checked {'uri': '/mgmt/tm/sys/folder/~test-virtual-tenant', 'status': 404}
    ```

## 异常处理

运行过程中出现错误时，会报`Failed`错误。

    ```shell
    $ python3 test.py --config=/root/env
    ->     : (1/71) virtual and pool        Testing /root/f5-kic/tests/funcs/test.automation/testcases/multiple-resources/01.virtual-pool.yaml
    ...    : (1/71) virtual and pool        Deploying ... kubectl --kubeconfig /root/kube.config.yaml apply -f /root/f5-kic/tests/funcs/test.automation/tmp-01.virtual-pool.yaml-virtual-and-pool.yaml
    OK     : (1/71) virtual and pool        configmap/a01-virtual-pool-yaml-template-configmap created
    OK     : (1/71) virtual and pool        deployment.apps/a01-virtual-pool-yaml-template-deployment created
    OK     : (1/71) virtual and pool        service/a01-virtual-pool-yaml-template created
    OK     : (1/71) virtual and pool
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 14)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 13)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 12)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 11)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 10)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 9)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 8)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 7)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 6)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 5)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 4)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 3)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 2)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 1)
    ...    : (1/71) virtual and pool        Checking https://10.250.18.105:8443/mgmt/tm/ltm/virtual/~mytenant~myapp~myvirtual (retrying time: 0)
    Failed : (1/71) virtual and pool        404 {"code":404,"message":"01020036:3: The requested Virtual Server (/mytenant/myapp/myvirtual) was not found.","errorStack":[],"apiError":3}
    ...    : ERROR                          Details
    ...    : ERROR                          yaml file: /root/f5-kic/tests/funcs/test.automation/tmp-01.virtual-pool.yaml-virtual-and-pool.yaml
    ```

当出现`Failed`错误时，

1. 观察最后执行的案例原始文件(`->`行所指向的yaml路径)：`/root/f5-kic/tests/funcs/test.automation/testcases/multiple-resources/01.virtual-pool.yaml`

2. 观察生成的被执行失败的yaml文件：`/root/f5-kic/tests/funcs/test.automation/tmp-01.virtual-pool.yaml-virtual-and-pool.yaml`

3. 观察ERROR日志，根据实际情况定义或者报告问题

   ```
   $ kubectl logs -f deployment/k8s-bigip-ctlr-c -c k8s-bigip-ctlr-c-pod -n kube-system
   ```

4. 执行删除命令，清理环境。

   ```shell
   $ kubectl --kubeconfig /root/kube.config.yaml delete -f /root/f5-kic/tests/funcs/test.automation/tmp-01.virtual-pool.yaml-virtual-and-pool.yaml
   ```

5. 问题确认后，也可以重新单独运行刚才出错的测试案例

    ```shell
    $ python3 test.py --config=/root/env testcases/multiple-resources/01.virtual-pool.yaml
    ```

   如果执行成功，则可重新执行全部测试用例：

   ```shell
   $ python3 test.py --config /root/env  # 即，不加任何案例
   ```


## 测试案例定义

参考 [00.test-automation-guide.yaml](testcases/00.test-automation-guide.yaml)