#!/bin/bash

# Test if CommitPeer advertises tx-sign protocol
# This script checks protocol negotiation

echo "Checking CommitPeer transport configuration..."
grep -r "SetStreamHandler\|AddrsFactory" commitingpeer/source/internal/deliver/ || echo "No SetStreamHandler found"

echo ""
echo "Checking if protocol is registered before/after Subscribe..."
grep -A 30 "RegisterTxSignHandler" commitingpeer/source/cmd/peer/main.go | head -20
