package adapters_test

import (
	"testing"

	"github.com/felinics/memoh/internal/channel"
	"github.com/felinics/memoh/internal/channel/adapters/dingtalk"
	"github.com/felinics/memoh/internal/channel/adapters/discord"
	"github.com/felinics/memoh/internal/channel/adapters/feishu"
	"github.com/felinics/memoh/internal/channel/adapters/line"
	localadapter "github.com/felinics/memoh/internal/channel/adapters/local"
	"github.com/felinics/memoh/internal/channel/adapters/matrix"
	"github.com/felinics/memoh/internal/channel/adapters/misskey"
	"github.com/felinics/memoh/internal/channel/adapters/qq"
	"github.com/felinics/memoh/internal/channel/adapters/slack"
	"github.com/felinics/memoh/internal/channel/adapters/telegram"
	"github.com/felinics/memoh/internal/channel/adapters/wechatoa"
	"github.com/felinics/memoh/internal/channel/adapters/wecom"
	"github.com/felinics/memoh/internal/channel/adapters/weixin"
)

var (
	_ channel.Sender = (*dingtalk.DingTalkAdapter)(nil)
	_ channel.Sender = (*discord.DiscordAdapter)(nil)
	_ channel.Sender = (*feishu.FeishuAdapter)(nil)
	_ channel.Sender = (*localadapter.WebAdapter)(nil)
	_ channel.Sender = (*matrix.MatrixAdapter)(nil)
	_ channel.Sender = (*misskey.MisskeyAdapter)(nil)
	_ channel.Sender = (*qq.QQAdapter)(nil)
	_ channel.Sender = (*telegram.TelegramAdapter)(nil)
	_ channel.Sender = (*wechatoa.WeChatOAAdapter)(nil)
	_ channel.Sender = (*wecom.WeComAdapter)(nil)
	_ channel.Sender = (*weixin.WeixinAdapter)(nil)

	_ channel.StreamSender = (*dingtalk.DingTalkAdapter)(nil)
	_ channel.StreamSender = (*discord.DiscordAdapter)(nil)
	_ channel.StreamSender = (*feishu.FeishuAdapter)(nil)
	_ channel.StreamSender = (*localadapter.WebAdapter)(nil)
	_ channel.StreamSender = (*matrix.MatrixAdapter)(nil)
	_ channel.StreamSender = (*misskey.MisskeyAdapter)(nil)
	_ channel.StreamSender = (*qq.QQAdapter)(nil)
	_ channel.StreamSender = (*telegram.TelegramAdapter)(nil)
	_ channel.StreamSender = (*wechatoa.WeChatOAAdapter)(nil)
	_ channel.StreamSender = (*wecom.WeComAdapter)(nil)
	_ channel.StreamSender = (*weixin.WeixinAdapter)(nil)

	_ channel.SelfIdentityPolicyProvider = (*dingtalk.DingTalkAdapter)(nil)
	_ channel.SelfIdentityPolicyProvider = (*discord.DiscordAdapter)(nil)
	_ channel.SelfIdentityPolicyProvider = (*feishu.FeishuAdapter)(nil)
	_ channel.SelfIdentityPolicyProvider = (*line.Adapter)(nil)
	_ channel.SelfIdentityPolicyProvider = (*matrix.MatrixAdapter)(nil)
	_ channel.SelfIdentityPolicyProvider = (*misskey.MisskeyAdapter)(nil)
	_ channel.SelfIdentityPolicyProvider = (*qq.QQAdapter)(nil)
	_ channel.SelfIdentityPolicyProvider = (*slack.SlackAdapter)(nil)
	_ channel.SelfIdentityPolicyProvider = (*telegram.TelegramAdapter)(nil)
	_ channel.SelfIdentityPolicyProvider = (*wechatoa.WeChatOAAdapter)(nil)
)

func TestEnabledChannelAdaptersRequireCredentialVerification(t *testing.T) {
	t.Parallel()

	reg := channel.NewRegistry()
	reg.MustRegister(telegram.NewTelegramAdapter(nil))
	reg.MustRegister(discord.NewDiscordAdapter(nil))
	reg.MustRegister(qq.NewQQAdapter(nil))
	reg.MustRegister(matrix.NewMatrixAdapter(nil))
	reg.MustRegister(feishu.NewFeishuAdapter(nil))
	reg.MustRegister(slack.NewSlackAdapter(nil))
	reg.MustRegister(wecom.NewWeComAdapter(nil))
	reg.MustRegister(dingtalk.NewDingTalkAdapter(nil))
	reg.MustRegister(wechatoa.NewWeChatOAAdapter(nil))
	reg.MustRegister(line.NewAdapter(nil))
	reg.MustRegister(weixin.NewWeixinAdapter(nil))
	reg.MustRegister(localadapter.NewWebAdapter(nil))
	reg.MustRegister(misskey.NewMisskeyAdapter(nil))

	required := []channel.ChannelType{
		telegram.Type,
		discord.Type,
		qq.Type,
		matrix.Type,
		feishu.Type,
		slack.Type,
		dingtalk.Type,
		wechatoa.Type,
		line.Type,
		misskey.Type,
	}
	for _, channelType := range required {
		if !reg.SelfIdentityPolicy(channelType).RequireDiscoveryOnEnable {
			t.Errorf("%s should require credential verification before enable", channelType)
		}
	}
	// Saving configuration must not open a competing subscription or
	// announce a client startup before the connection lifecycle takes over.
	for _, channelType := range []channel.ChannelType{localadapter.WebType, wecom.Type, weixin.Type} {
		if reg.SelfIdentityPolicy(channelType).RequireDiscoveryOnEnable {
			t.Errorf("%s should not require platform credential discovery before save", channelType)
		}
	}
}
