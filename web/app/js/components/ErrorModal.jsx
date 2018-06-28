import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { Button, Icon, Modal, Switch } from 'antd';

// max characters we displaly for error messages before truncating them
const maxErrorLength = 500;

export default class ErrorModal extends React.Component {
  static propTypes = {
    errors: PropTypes.shape({}).isRequired,
    resourceName: PropTypes.string.isRequired,
    resourceType: PropTypes.string.isRequired
  }

  state = {
    visible: false,
    truncateErrors: true
  }

  showModal = () => {
    this.setState({
      visible: true,
    });
  }

  handleOk = () => {
    this.setState({
      visible: false,
    });
  }

  handleCancel = () => {
    this.setState({
      visible: false,
    });
  }

  toggleTruncateErrors = () => {
    this.setState({
      truncateErrors: !this.state.truncateErrors
    });
  }

  processErrorData = podErrors => {
    let shouldTruncate = false;
    let byPodAndContainer = _(podErrors)
      .keys()
      .sortBy()
      .map(pod => {
        return {
          pod: pod,
          byContainer: _(podErrors[pod].errors)
            .groupBy( "container.container")
            .mapValues(v => {
              return _.map(v, err => {
                let errMsg = _.get(err, ["container", "message"]);

                if (!errMsg) {
                  return null;
                }

                if (_.size(errMsg) > maxErrorLength) {
                  shouldTruncate = true;
                  err.container.truncatedMessage = _.take(errMsg, maxErrorLength).join("") + "...";
                }

                return err.container;
              });
            })
            .compact()
            .value()
        };
      }).value();

    return {
      byPodAndContainer,
      shouldTruncate
    };
  }

  renderContainerErrors = (pod, errorsByContainer) => {
    if (_.isEmpty(errorsByContainer)) {
      return "No messages to display";
    }

    return _.map(errorsByContainer, (errors, container) => (
      <div key={`error-${container}`}>
        <div className="clearfix">
          <span className="pull-left" title="container name">{container}</span>
          <span className="pull-right" title="docker image">{_.get(errors, [0, "image"])}</span>
        </div>

        <div className="error-text">
          {
            _.map(errors, (er, i) => {
                if (_.size(er.message) === 0) {
                  return null;
                }

                let message = !this.state.truncateErrors ? er.message :
                  er.truncatedMessage || er.message;

                return (
                  <React.Fragment  key={`error-msg-long-${i}`}>
                    <code>{message}</code><br /><br />
                  </React.Fragment>
                );
              }
            )
          }
        </div>
      </div>
    ));
  };

  renderPodErrors = errors => {
    return _.map(errors, err => {
      return (
        <div className="conduit-pod-error" key={err.pod}>
          <h3 title="pod name">{err.pod}</h3>
          {this.renderContainerErrors(err.pod, err.byContainer)}
        </div>
      );
    });
  }

  render() {
    let errors = this.processErrorData(this.props.errors);

    return (
      <React.Fragment>
        <Icon type="warning" className="conduit-error-icon" onClick={this.showModal} />
        <Modal
          className="conduit-pod-error-modal"
          title={`Errors in ${this.props.resourceType} ${this.props.resourceName}`}
          visible={this.state.visible}
          onOk={this.handleOk}
          onCancel={this.handleCancel}
          footer={<Button key="modal-ok" type="primary" onClick={this.handleOk}>OK</Button>}
          width="800px">
          <React.Fragment>
            {
              errors.shouldTruncate ?
                <React.Fragment>
                  Some of these error messages are very long. Show full error text? <Switch onChange={this.toggleTruncateErrors} />
                </React.Fragment> : null
            }
            {this.renderPodErrors(errors.byPodAndContainer)}
          </React.Fragment>
        </Modal>
      </React.Fragment>
    );
  }
}
