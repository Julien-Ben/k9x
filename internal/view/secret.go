// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/config/data"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/tcell/v2"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// Secret presents a secret viewer.
type Secret struct {
	ResourceViewer
}

// NewSecret returns a new viewer.
func NewSecret(gvr *client.GVR) ResourceViewer {
	s := Secret{
		ResourceViewer: NewOwnerExtender(NewBrowser(gvr)),
	}
	s.AddBindKeysFn(s.bindKeys)

	return &s
}

func (s *Secret) bindKeys(aa *ui.KeyActions) {
	aa.Bulk(ui.KeyMap{
		ui.KeyX: ui.NewKeyAction("Decode", s.decodeCmd, true),
		ui.KeyU: ui.NewKeyAction("UsedBy", s.refCmd, true),
	})
}

func (s *Secret) refCmd(evt *tcell.EventKey) *tcell.EventKey {
	return scanRefs(evt, s.App(), s.GetTable(), client.SecGVR)
}

func (s *Secret) decodeCmd(evt *tcell.EventKey) *tcell.EventKey {
	path := s.GetTable().GetSelectedItem()
	if path == "" {
		return evt
	}

	ctx := contextForScope(s.GetTable().selectedContext())
	var (
		o   runtime.Object
		err error
	)
	if factory, ok := s.App().factory.(dao.ContextualFactory); ok {
		o, err = factory.GetWithContext(ctx, s.GVR(), path, true, labels.Everything())
	} else {
		o, err = s.App().factory.Get(s.GVR(), path, true, labels.Everything())
	}
	if err != nil {
		s.App().Flash().Err(err)
		return nil
	}

	mm, err := dao.ExtractSecrets(o)
	if err != nil {
		s.App().Flash().Err(err)
		return nil
	}

	raw, err := data.WriteYAML(mm)
	if err != nil {
		s.App().Flash().Errf("Error decoding secret %s", err)
		return nil
	}

	details := NewDetails(s.App(), "Secret Decoder", path, contentYAML, true).Update(string(raw))
	if err := s.App().inject(details, false); err != nil {
		s.App().Flash().Err(err)
	}

	return nil
}
