(tile mqtt
  (offers event-bus)
  (doc "MQTT: a topic per event type on a broker, published and subscribed at QoS 1")
  (type-name MQTTBus)
  (engine mqtt)
  (import mqtt github.com/eclipse/paho.mqtt.golang)
  (topics mqtt)

  (satisfies (cross-process)
    "every client connected to the broker can subscribe")
  (satisfies (fan-out)
    "every subscriber to a topic gets each message")
  (illegal-when (replay)
    "a broker hands a message on and keeps no log to read back, beyond one retained message per topic")
  (illegal-when (no-broker)
    "an MQTT broker to run")
  (unverified (durable)
    "a broker holds a QoS 1 message for a subscriber that is away only if that client connected with a persistent session, and whether it survives a broker restart is the broker's configuration")
  (unverified (ordered)
    "a broker delivers a topic's messages to a subscriber in the order it took them, and a redelivery after a reconnect can repeat one out of place; nothing here checks")
  (unverified (at-least-once)
    "QoS 1 redelivers until the client acknowledges, and this client acknowledges when the callback returns, whether or not the handler succeeded")
  (unverified (consumer-groups)
    "sharing a topic among a group is a shared subscription ($share/GROUP/TOPIC), part of MQTT 5 and a broker extension before it; this client speaks MQTT 3.1.1")

  (cost
    (llm-work 4 (source derived "tokens to wait on, a callback that cannot return an error, and JSON by hand"))
    (maintenance 4 (source derived "topic names and QoS to keep consistent, and a handler failure that the broker never hears about"))
    (dependency 2 (source derived "the Paho client and its websocket and sync modules"))
    (runtime 2 (source derived "a round trip and an acknowledgement per message"))
    (operations 4 (source derived "a broker to run, with sessions and retained messages to size"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by an MQTT broker. Each event type is a topic\nunder a prefix, published and subscribed at QoS 1.")
  (holds (client mqtt.Client) (prefix string))
  (about-new
    "New{impl} returns a {impl} over a connected client, with every topic\nunder prefix.")
  (new (params (client mqtt.Client) (prefix string))
    "return &{impl}{client: client, prefix: prefix}")

  (hint publish
    "Encode e as JSON, call token := s.client.Publish(s.prefix+\"{event}\", 1, false, the bytes), then token.Wait() and return token.Error(). QoS 1, not retained.")
  (hint on
    "Call token := s.client.Subscribe(s.prefix+\"{event}\", 1, func(_ mqtt.Client, m mqtt.Message) {...}), decoding m.Payload() as JSON into {event} and calling h. The callback cannot return an error and the message is acknowledged when it returns, so a failure from h is one to record, not one the broker will redeliver. Wait on the token and return token.Error() if it is set. Return a func that calls s.client.Unsubscribe(the same topic) and waits on its token."))
