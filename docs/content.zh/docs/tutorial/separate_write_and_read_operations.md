---
title: "基于网关的读写分离设置"
weight: 30
draft: true
---

# 基于网关的读写分离设置

当前一个查询发送给 Elasticsearch 的时候，Elasticsearch 默认会随机挑选一份数据来进行检索，可能是主分片，也可能是副本分片。
在某些场景下，我们可能希望实现请求的读写分离，一般做法可能需要在客户端手动选择不同的目标节点，实现起来比较麻烦，而通过网关可以非常方便的实现请求的读写分离。

## 基于 `preference` 参数

Elasticsearch 提供了一个非常好用的 `preference` 参数，可以控制请求的优先访问的目标资源，支持如下参数：
| 名称 | 说明 |
| --------- | ------------------------------------------------------------ |
| _only_local | 只访问本地节点上有的分片数据 |
| \_local | 如果本地节点包含相关分片数据，则优先访问本地节点的分片数据，否则转发到相关节点 |
| \_only_nodes:<node-id>,<node-id> | 只访问特定节点上面的分片数据，当分片不在指定节点上才转发到相关节点 |
| \_prefer_nodes:<node-id>,<node-id> | 尽可能优先的访问指定节点上的分片数据 |
| \_shards:<shard>,<shard> | 只访问特定编号的分片数据 |
| <custom-string>| 任何自定义的字符串(不能是`_`开头)，包含相同该值的请求都以相同的顺序来访问这些分片 |
