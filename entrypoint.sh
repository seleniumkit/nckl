#!/bin/bash
/usr/bin/nckl -listen ":$PORT" -users ${USERS_FILE} -quotaDir ${QUOTA_DIRECTORY} -destination ${DESTINATION}
