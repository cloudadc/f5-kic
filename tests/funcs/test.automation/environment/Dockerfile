# docker build . -t zongzw/ansible-env:latest-2022.06.23

FROM centos:7
ENV LANG=en_US.utf-8
ENV LC_ALL=en_US.utf-8
RUN yum install -y python3-pip
RUN pip3 install --upgrade pip==20.3.4
RUN pip3 install \
        urllib3==1.26.9 \
        jmespath==0.10.0 \
        requests==2.27.1 \
        jinja2==3.0.3 \
        PyYAML==6.0
RUN curl -LO https://dl.k8s.io/release/v1.24.0/bin/linux/amd64/kubectl
RUN install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
