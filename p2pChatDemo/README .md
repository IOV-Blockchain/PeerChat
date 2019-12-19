利用以太坊p2p网络底层写的p2p聊天demo

## p2pChatDemo

###功能与使用
* 利用以太坊的底层p2p网络代码，实现的一个聊天系统。
* 程序启动时只需要利用ethereum的bootnode来生成一个静态节点，首先生成nodekey文件，./bootnode -genkey bootnode.key，再启动一个静态节点，./bootnode -nodekey=bootnode.key --addr 启动机器ip地址:端口 -nodekey=bootnode.key。静态节点启动之后会生成一个一个静态地址，静态地址的尾部结构为self=enode://...,将enode://...开头的静态地址复制下来。
* 启动p2pChatDemo,./p2pChatDemo --bootnode encode://... --port 10001，当启动时没有添加port参数时，默认端口为10001。
* 当各节点都启动网路连接了之后，直接在终端上输入就可以广播给其他节点接收到消息。当发送消息时p2p peerAddr 消息体，就会直接点对点发送到某个节点。

 
