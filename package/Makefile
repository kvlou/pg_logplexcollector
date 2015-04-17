WORKDIR := $(shell mktemp -d)
MKDIR := $(shell dirname $(MAKEFILE_LIST))
SRCDIR := $(shell dirname ${MKDIR})
PROG := pg_logplexcollector
VERSION := $(shell cat ${MKDIR}/../VERSION)
DISTRO ?= ubuntu/precise

default: package

prepare: clean
	mkdir -p ${WORKDIR}
	mkdir -p ${WORKDIR}/p/usr/bin
	mkdir -p ${WORKDIR}/p/etc/init
	mkdir -p ${MKDIR}/${DISTRO}
	
build: prepare
	cd ${SRCDIR} && godep go build -o ${PROG}
	
install: build
	install -c ${MKDIR}/pre-install.sh ${WORKDIR}/pre-install.sh
	install -c ${MKDIR}/post-install.sh ${WORKDIR}/post-install.sh
	install -c ${MKDIR}/before-remove.sh ${WORKDIR}/before-remove.sh
	install -c ${SRCDIR}/${PROG} ${WORKDIR}/p/usr/bin/${PROG}
	install -c ${MKDIR}/${PROG}.conf ${WORKDIR}/p/etc/init/${PROG}.conf
	
package: install
	cd ${WORKDIR} && \
  	fpm -s dir -t deb -n ${PROG} -v ${VERSION} \
		    --package ${MKDIR}/${DISTRO} \
		    --pre-install ./pre-install.sh \
		    --post-install ./post-install.sh \
		    --before-remove ./before-remove.sh \
		    ./p/usr/bin/${PROG}=/usr/bin/${PROG} \
		    ./p/etc/init/${PROG}.conf=/etc/init/${PROG}.conf

clean: 
	rm -f ${SRCDIR}/${PROG}
	rm -f ${MKDIR}/*.deb