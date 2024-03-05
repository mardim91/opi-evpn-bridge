package config

import (
	"log"

	"github.com/spf13/viper"
)

type SubscriberConfig struct {
	Name     string   `yaml:"name"`
	Priority int      `yaml:"priority"`
	Events   []string `yaml:"events"`
}
type P4FilesConfig struct {
	P4info_file string `yaml:"p4info_file"`
	Bin_file    string `yaml:"bin_file"`
	Conf_file   string `yaml:"conf_file"`
}
type RepresentorsConfig struct {
	Port_mux  string `yaml:"port-mux"`
	Vrf_mux   string `yaml:"vrf_mux"`
	Grpc_acc  string `yaml:"host"`
	Grpc_host string `yaml:"grpc_host"`
	Phy0_rep  string `yaml:"phy0_rep"`
	Phy1_rep  string `yaml:"phy1_rep"`
}
type P4_Config struct {
	Enabled      bool                   `yaml:"enabled"`
	Driver       string                 `yaml:"driver"`
	Representors map[string]interface{} `yaml:"representors"`
	Config       P4FilesConfig          `yaml:"config"`
}
type loglevelConfig struct {
	Db        string `yaml:"db"`
	Grpc      string `yaml:"grpc"`
	Linux_frr string `yaml:"linux_frr"`
	Netlink   string `yaml:"netlink"`
	P4        string `yaml:"p4"`
}
type Linux_frrConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Default_vtep string `yaml:"default_vtep"`
	Port_mux     string `yaml:"port_mux"`
	Vrf_mux      string `yaml:"vrf_mux"`
	Ip_mtu       int    `yaml:"ip_mtu"`
}
type Netlink_Config struct {
	Enabled       bool `yaml:"enabled"`
	Poll_interval int  `yaml:"poll_interval"`
	Phy_ports     []struct {
		Name string `yaml:"name"`
		Vsi  int    `yaml:"vsi"`
	} `yaml:"phy_ports"`
}
type Config struct {
	CfgFile     string
	GRPCPort    int                `yaml:"grpcport"`
	HTTPPort    int                `yaml:"httpport"`
	TLSFiles    string             `yaml:"tlsfiles"`
	Database    string             `yaml:"database"`
	DBAddress   string             `yaml:"dbaddress"`
	FRRAddress  string             `yaml:"frraddress"`
	Buildenv    string             `yaml:"buildenv"`
	Subscribers []SubscriberConfig `yaml:"subscribers"`
	Linux_frr   Linux_frrConfig    `yaml:"linux_frr"`
	Netlink     Netlink_Config     `yaml:"netlink"`
	P4          P4_Config          `yaml: "p4"`
	LogLevel    loglevelConfig     `yaml: "loglevel"`
}

var GlobalConfig Config

func SetConfig(cfg Config) error {
	GlobalConfig = cfg
	return nil
}

func LoadConfig() {
	/*if GlobalConfig.CfgFile != "" {
		viper.SetConfigFile(GlobalConfig.CfgFile)
	} else {
		// Search config in the default location
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName("evpn.yaml")
	}*/
	if err := viper.ReadInConfig(); err == nil {
		log.Println("Using config file:", viper.ConfigFileUsed())
	}
	log.Println("Load Config Function")
	if err := viper.Unmarshal(&GlobalConfig); err != nil {
		log.Println(err)
		return
	}
	/*GlobalConfig.P4.Enable = viper.GetBool("p4.enabled")
	GlobalConfig.Linux_frr.Enable = viper.GetBool("linux_frr.enabled")
	GlobalConfig.Netlink.Enable = viper.GetBool("netlink.enabled")*/

	log.Println("config %+v", GlobalConfig)
	log.Printf("enabled from init config: %d\n", GlobalConfig.P4.Enabled)
}

func GetConfig() *Config {
	return &GlobalConfig
}
