# wget rpm to local

appsvcs_rpm=f5-appsvcs-3.36.0-6.noarch.rpm
cdir=`cd $(dirname $0); pwd`
echo $cdir
if [ ! -f $cdir/$appsvcs_rpm ]; then
    (
        cd $cdir
        wget https://github.com/F5Networks/f5-appsvcs-extension/releases/download/v3.36.0/f5-appsvcs-3.36.0-6.noarch.rpm
    )
fi


docker run -v $cdir:/rpms centos:7 bash -c "\
    cd /rpms; rpm2cpio $appsvcs_rpm | cpio -div;"

(
    cd $cdir
    rm -rf f5-appsvcs
    mv ./var/config/rest/iapps/f5-appsvcs .
    rm -rf ./var
)