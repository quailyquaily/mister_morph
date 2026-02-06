MM=./bin/mistermorph

  A_DIR=~/.morph/maep
  B_DIR=~/.morph/maep2
  # rm -rf "$A_DIR" "$B_DIR"

  # $MM maep --dir "$A_DIR" init
  # $MM maep --dir "$B_DIR" init

  A_PEER=$($MM maep --dir "$A_DIR" id --json | jq -r '.peer_id')
  B_PEER=$($MM maep --dir "$B_DIR" id --json | jq -r '.peer_id')

  $MM maep --dir "$A_DIR" card export \
    --address "/ip4/127.0.0.1/tcp/4101/p2p/$A_PEER" \
    --out /tmp/a.card.json

  $MM maep --dir "$B_DIR" card export \
    --address "/ip4/127.0.0.1/tcp/4102/p2p/$B_PEER" \
    --out /tmp/b.card.json

  $MM maep --dir "$A_DIR" contacts import /tmp/b.card.json
  $MM maep --dir "$B_DIR" contacts import /tmp/a.card.json
