## setup and dev

```bash
sudo useradd -r -s /usr/sbin/nologin pigate
sudo usermod -aG netdev pigate
sudo setfacl -m u:pigate:rwx /etc/wpa_supplicant
sudo setfacl -d -m u:pigate:rwx /etc/wpa_supplicant
sudo nano /etc/polkit-1/rules.d/10-pigate-wpa.rules
```

```bash
# for /etc/polkit-1/rules.d/10-pigate-wpa.rules
polkit.addRule(function(action, subject) {
    if (action.id == "org.freedesktop.systemd1.manage-units" &&
        action.lookup("unit").indexOf("wpa_supplicant@") === 0 &&
        subject.user == "pigate") {
        return polkit.Result.YES;
    }
});
```

```bash
sudo mkdir -p /var/lib/pigate
sudo mkdir -p /run/pigate
sudo chown -R pigate:netdev /var/lib/pigate
sudo chown -R pigate:pigate /run/pigate
sudo chmod 775 /var/lib/pigate

sudo nano /etc/sudoers.d/pigate
# pigate ALL=(ALL) NOPASSWD: /usr/sbin/dhclient, /usr/sbin/dhcpcd, /usr/bin/systemctl

sudo setcap cap_net_admin,cap_net_raw+ep ./pigate

sudo -u pigate ./pigate -mock=false -db=/var/lib/pigate/pigate.db
```

## install

```bash
sudo cp ./pigate /usr/local/bin/.
sudo -u pigate pigate -mock=false -db=/var/lib/pigate/pigate.db
```

# More

```bash
sudo rm /var/run/wpa_supplicant/wlx0cef1548ff2b

```