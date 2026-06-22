```bash
-P INPUT DROP
-N sapray-not-local
-A INPUT -m conntrack --ctstate INVALID -j DROP
-A INPUT -p icmp -m icmp --icmp-type 3 -j ACCEPT
-A INPUT -p icmp -m icmp --icmp-type 11 -j ACCEPT
-A INPUT -p icmp -m icmp --icmp-type 12 -j ACCEPT
-A INPUT -p icmp -m icmp --icmp-type 8 -j ACCEPT
-A INPUT -p udp -m udp --dport 137 -j DROP
-A INPUT -p udp -m udp --dport 138 -j DROP
-A INPUT -p tcp -m tcp --dport 139 -j DROP
-A INPUT -p tcp -m tcp --dport 445 -j DROP
-A INPUT -p udp -m udp --dport 68 -j DROP
-A INPUT -p udp -m udp --dport 67 -j DROP
-A INPUT -m addrtype --dst-type BROADCAST -j DROP
-A INPUT -j sapray-not-local
-A INPUT -d 224.0.0.251/32 -p udp -m udp --dport 5353 -j ACCEPT
-A INPUT -d 239.255.255.250/32 -p udp -m udp --dport 1900 -j ACCEPT

# Docker interface
-A INPUT -i br-2919cae5ffc9 -j ACCEPT
-A INPUT -i br-371d09a45456 -j ACCEPT

-A INPUT -i lo -j ACCEPT
-A INPUT -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT

-A INPUT -j LOG --log-prefix "[SFW] INP AUDIT : "
# User config rules here
# -A INPUT <rule>
-A INPUT -j LOG --log-prefix "[SFW] INP DROP  : "


-A sapray-not-local -m addrtype --dst-type LOCAL -j RETURN
-A sapray-not-local -m addrtype --dst-type MULTICAST -j RETURN
-A sapray-not-local -m addrtype --dst-type BROADCAST -j RETURN
-A sapray-not-local -m limit --limit 3/min --limit-burst 10 -j LOG --log-prefix "[SFW] INP DROP  : "
-A sapray-not-local -j DROP

```

