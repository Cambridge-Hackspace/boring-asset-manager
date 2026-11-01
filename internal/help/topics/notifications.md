# Notifications

The **Notifications** page sends news to chat or other services: when stock runs low, when something is overdue, and other events you choose. Each channel says what it subscribes to, and you can send a test to check that it works. Deliveries that fail are retried.

Channels can go to **Discord** or to an **MQTT** broker, such as Home Assistant's. An MQTT channel publishes each event to a topic under its prefix, such as `bam/asset.hook/…` for a [hook](/help/hooks) triggered. Give it the broker's address as `mqtts://` to encrypt the connection, the user name and password the broker asks for, and, for a broker with a certificate of its own, its CA certificate.
