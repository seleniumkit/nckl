#!/bin/bash
USERS_FILE=${USERS_FILE:-"/etc/grid-router/users.properties"}
QUOTA_DIRECTORY=${QUOTA_DIRECTORY:-"/etc/grid-router/quota"}
DESTINATION=${DESTINATION:-"localhost:4444"}
/usr/bin/nckl -usersFile $USERS_FILE -quotaDirectory $QUOTA_DIRECTORY -destination $DESTINATION
