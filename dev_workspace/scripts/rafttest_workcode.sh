#!/usr/bin/env bash

set -e

BASE_PATH=$(cd "$(dirname "$0")/../.." && pwd)
DEV_PATH=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT_PATH="$DEV_PATH/scripts"

rm "$BASE_PATH/rafttest.log" 2> /dev/null || true

while ((i<11))
do
   N=$(((RANDOM % 10000) + 10000))
   echo "${A[*]}" | grep $N && continue
   A[$i]=$N
   ((i++))
done

NODE_PORT0=${A[0]}
NODE_PORT1=${A[1]}
NODE_PORT2=${A[2]}
NODE_PORT3=${A[3]}
NODE_PORT4=${A[4]}
PROXY_NODE_PORT0=${A[5]}
PROXY_NODE_PORT1=${A[6]}
PROXY_NODE_PORT2=${A[7]}
PROXY_NODE_PORT3=${A[8]}
PROXY_NODE_PORT4=${A[9]}
TESTER_PORT=${A[10]}
ALL_PORTS=" ${NODE_PORT0} ${NODE_PORT1} ${NODE_PORT2} ${NODE_PORT3} ${NODE_PORT4}"
ALL_PROXY_PORTS=" ${PROXY_NODE_PORT0} ${PROXY_NODE_PORT1} ${PROXY_NODE_PORT2} ${PROXY_NODE_PORT3} ${PROXY_NODE_PORT4}"

echo "All real ports:" ${ALL_PORTS}
echo "All proxy ports:" ${ALL_PROXY_PORTS}

"$SCRIPT_PATH/rafttest_single_workcode.sh" testOneCandidateOneRoundElection ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
"$SCRIPT_PATH/rafttest_single_workcode.sh" testOneCandidateStartTwoElection ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
"$SCRIPT_PATH/rafttest_single_workcode.sh" testTwoCandidateForElection ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
"$SCRIPT_PATH/rafttest_single_workcode.sh" testSplitVote ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
"$SCRIPT_PATH/rafttest_single_workcode.sh" testAllForElection ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
"$SCRIPT_PATH/rafttest_single_workcode.sh" testLeaderRevertToFollower ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
"$SCRIPT_PATH/rafttest_single_workcode.sh" testOneSimplePut ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
"$SCRIPT_PATH/rafttest_single_workcode.sh" testOneSimpleUpdate ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
"$SCRIPT_PATH/rafttest_single_workcode.sh" testOneSimpleDelete ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
"$SCRIPT_PATH/rafttest_single_workcode.sh" testDeleteNonExistKey ${ALL_PORTS} ${ALL_PROXY_PORTS} ${TESTER_PORT}
