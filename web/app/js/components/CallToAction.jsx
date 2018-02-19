import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import React from 'react';
import './../../css/cta.css';

export default class CallToAction extends React.Component {
  render() {
    return (
      <div className="call-to-action">
        <div className="action summary">The service mesh was successfully installed!</div>

        <div className="action-steps">
          <div className="step-container complete">
            <div className="icon-container">
              <i className="fa fa-check-circle" aria-hidden="true" />
            </div>
            <div className="message"><p>Controller successfully installed</p></div>
          </div>

          <div className="step-container complete">
            <div className="icon-container">
              <i className="fa fa-check-circle" aria-hidden="true" />
            </div>
            <div className="message">{this.props.numDeployments || 0} deployments detected</div>
          </div>

          <div className="step-container incomplete">
            <div className="icon-container">
              <i className="fa fa-circle-o" aria-hidden="true" />
            </div>
            <div className="message">Connect your first deployment</div>
          </div>
        </div>

        <div className="clearfix">
          {incompleteMeshMessage()}
        </div>
      </div>
    );
  }
}
